package moderation

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// api_key_health.go selects moderation credentials and records their health and
// synchronous request load. Each service retains hash-addressed observations
// and accumulated scheduling weights while configurations supply the current
// credential order and priorities. Selection skips active freezes, updates
// eligible weights under the health mutex, and resolves equal weights in
// configured order. Replacing configuration leaves the accumulated weights
// available for credentials that remain or later return.
//
// Attempt accounting and health publication acquire the same mutex separately.
// Administrator views also sample each key's status and counters separately, so
// concurrent calls can change observations between those reads. Freeze deadlines
// follow the latest freezable result, including replacements that shorten an
// earlier deadline; success clears both exclusion and the retained error.

// Credential exclusion durations distinguish authentication, throttling, and
// other HTTP failures before later selections reconsider the affected key.
const (
	contentModerationKeyRateLimitFreezeDuration = time.Minute
	contentModerationKeyAuthFreezeDuration      = 10 * time.Minute
	contentModerationKeyHTTPErrorFreezeDuration = 10 * time.Second
)

// contentModerationKeyHealth stores mutable credential observations and
// synchronous counters under the service key-health mutex.
type contentModerationKeyHealth struct {
	Hash           string    // normalized credential digest identity
	Masked         string    // credential tail display mask
	FailureCount   int       // freezable failures since success
	SuccessCount   int64     // cumulative successful attempt count
	LastError      string    // latest truncated diagnostic text
	LastCheckedAt  time.Time // latest observed attempt instant
	FrozenUntil    time.Time // credential exclusion end instant
	LastLatencyMS  int       // milliseconds
	LastHTTPStatus int       // latest observed HTTP status
	LastTested     bool      // an attempt has been observed
	// SyncActive counts begun attempts until finish records an outcome. SyncTotal,
	// SyncSuccess, SyncErrors, and SyncLatencyMS accumulate completed attempts;
	// repeated finishes still add outcomes even after activity reaches zero.
	SyncActive    int64 // tracked synchronous in-flight attempts
	SyncTotal     int64 // tracked synchronous completed attempts
	SyncSuccess   int64 // tracked synchronous successful attempts
	SyncErrors    int64 // tracked synchronous failed attempts
	SyncLatencyMS int64 // cumulative synchronous attempt milliseconds
}

// contentModerationAPIKeyRuntimeEntry pairs a configured credential with its
// normalized hash-addressed scheduling attributes.
type contentModerationAPIKeyRuntimeEntry struct {
	Key      string // literal provider authentication credential
	KeyHash  string // normalized credential digest identity
	Priority int    // positive smooth scheduling weight
	Note     string // administrator credential display note
}

// nextUsableAPIKey returns one configured credential by smooth weighted
// scheduling while skipping active freezes. Missing services or entries return
// false. Under keyHealthMu, every eligible key adds its priority to accumulated
// scheduling weight, and the largest resulting weight wins with configured
// order deciding ties.
//
// The selected key subtracts total eligible weight before the lock is released.
// Frozen keys receive no addition during that selection, so available weights
// determine scheduling at each call. Existing scheduling entries remain across
// configuration changes; the method selects availability but does not itself
// begin an active-call counter.
func (s *ContentModerationService) nextUsableAPIKey(cfg *ContentModerationConfig) (string, bool) {
	entries := cfg.apiKeyEntries()
	if len(entries) == 0 || s == nil {
		return "", false
	}
	now := time.Now()
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	if s.keySchedule == nil {
		s.keySchedule = make(map[string]int64)
	}
	// Add weights only for currently unfrozen entries, then subtract their total
	// from the chosen key so subsequent selections spread the configured
	// eligible weight.
	var selected *contentModerationAPIKeyRuntimeEntry
	var selectedWeight int64
	var totalWeight int64
	for index := range entries {
		entry := &entries[index]
		state := s.keyHealth[entry.KeyHash]
		if state != nil && state.FrozenUntil.After(now) {
			continue
		}
		weight := int64(entry.Priority)
		totalWeight += weight
		s.keySchedule[entry.KeyHash] += weight
		current := s.keySchedule[entry.KeyHash]
		if selected == nil || current > selectedWeight {
			selected = entry
			selectedWeight = current
		}
	}
	if selected == nil || totalWeight <= 0 {
		return "", false
	}
	s.keySchedule[selected.KeyHash] -= totalWeight
	return selected.Key, true
}

// beginModerationAPIKeyCall increments tracked synchronous activity under the
// key-health mutex. Blank keys or nil services are ignored; callModerationBatch
// pairs an accepted begin with finishModerationAPIKeyCall after its audit
// attempt returns.
func (s *ContentModerationService) beginModerationAPIKeyCall(key string) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	// Publish activity before the transport runs so status readers can observe
	// an attempt that has no completed outcome yet.
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	state.SyncActive++
}

// finishModerationAPIKeyCall records one synchronous attempt result and
// nonnegative latency. It decreases positive active count, then increments
// totals and success or error counts under keyHealthMu; repeated calls still
// add outcomes and are not idempotent.
func (s *ContentModerationService) finishModerationAPIKeyCall(key string, latencyMS int, success bool) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	// Clamp invalid latency observations before adding to cumulative
	// whole-millisecond totals.
	if latencyMS < 0 {
		latencyMS = 0
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	// Complete activity and outcome counters together so a load reader cannot
	// observe only part of this attempt's accounting update.
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if state.SyncActive > 0 {
		state.SyncActive--
	}
	state.SyncTotal++
	state.SyncLatencyMS += int64(latencyMS)
	if success {
		state.SyncSuccess++
		return
	}
	state.SyncErrors++
}

// markAPIKeySuccess publishes a successful credential observation under
// keyHealthMu. It clears failure count, diagnostic error, and freeze,
// increments cumulative success, and records latency/status; blank credentials
// or nil services contribute nothing.
func (s *ContentModerationService) markAPIKeySuccess(key string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	// A successful observation reopens this key and clears prior error state
	// under the same lock used by freeze checks and weighted selection.
	state.FailureCount = 0
	state.SuccessCount++
	state.LastError = ""
	state.LastCheckedAt = time.Now()
	state.FrozenUntil = time.Time{}
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
}

// markAPIKeyError records the latest credential diagnostic and status under
// keyHealthMu. Freezable statuses increment failures and set a new freeze;
// status zero and 400 retain any earlier freeze instead of extending it. Blank
// credentials and nil services are ignored.
//
// Each freezable result replaces the deadline using a new clock sample, even
// when an earlier freeze ends later. A throttling result can therefore shorten
// an authentication freeze. Nonfreezable errors still replace LastError and
// attempt fields, so the displayed error can coexist with an older active
// freeze.
func (s *ContentModerationService) markAPIKeyError(key string, errText string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	// Record failure accounting with the attempt diagnostics before publishing
	// any replacement freeze, keeping the full observation under one lock.
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if contentModerationFreezeDurationForHTTPStatus(httpStatus) > 0 {
		state.FailureCount++
	}
	// Keep a bounded administrator diagnostic from this attempt.
	state.LastError = trimRunes(errText, 180)
	state.LastCheckedAt = time.Now()
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
	// Publish status-based exclusion with health diagnostics under one lock; zero
	// and request-shape failures leave any existing freeze instant as
	// previously observed.
	if freezeDuration := contentModerationFreezeDurationForHTTPStatus(httpStatus); freezeDuration > 0 {
		state.FrozenUntil = time.Now().Add(freezeDuration)
	}
}

// apiKeyStatuses returns per-key administrator health in normalized configured
// order. Each entry reads health independently under its mutex; missing keys
// produce an empty slice and caller changes to the returned entries do not
// alter stored health.
func (s *ContentModerationService) apiKeyStatuses(cfg *ContentModerationConfig) []ContentModerationAPIKeyStatus {
	entries := cfg.apiKeyEntries()
	out := make([]ContentModerationAPIKeyStatus, 0, len(entries))
	for idx, entry := range entries {
		out = append(out, s.apiKeyStatusForHash(idx, entry.KeyHash, maskSecretTail(entry.Key), true, entry.Priority, entry.Note))
	}
	return out
}

// preBlockAPIKeyActive sums sampled synchronous activity for the currently
// configured credential entries. Each load is read independently, so this
// total can combine observations from different moments during concurrent calls.
func (s *ContentModerationService) preBlockAPIKeyActive(cfg *ContentModerationConfig) int64 {
	var total int64
	for _, item := range s.preBlockAPIKeyLoads(cfg) {
		total += item.Active
	}
	return total
}

// preBlockAPIKeyAvailableCount counts configured credentials whose freeze is
// not after one sampled instant. Each health read uses its own lock
// acquisition, so concurrent health updates can alter membership while the
// count is collected.
func (s *ContentModerationService) preBlockAPIKeyAvailableCount(cfg *ContentModerationConfig) int64 {
	now := time.Now()
	var count int64
	for _, entry := range cfg.apiKeyEntries() {
		if !s.isAPIKeyFrozen(entry.Key, now) {
			count++
		}
	}
	return count
}

// preBlockAPIKeyTotalCalls sums sampled synchronous completed-attempt counts
// for currently configured credentials. Each load is read independently, so
// completed calls can appear between the samples contributing to this total.
func (s *ContentModerationService) preBlockAPIKeyTotalCalls(cfg *ContentModerationConfig) int64 {
	var total int64
	for _, item := range s.preBlockAPIKeyLoads(cfg) {
		total += item.Total
	}
	return total
}

// preBlockAPIKeyLoads returns tracked synchronous load observations in
// configured key order. Each key receives its own status and load samples, so
// concurrent updates can occur between entries and between a key status and
// counters.
func (s *ContentModerationService) preBlockAPIKeyLoads(cfg *ContentModerationConfig) []ContentModerationAPIKeyLoad {
	entries := cfg.apiKeyEntries()
	out := make([]ContentModerationAPIKeyLoad, 0, len(entries))
	for idx, entry := range entries {
		out = append(out, s.preBlockAPIKeyLoadForHash(idx, entry.KeyHash, maskSecretTail(entry.Key), entry.Priority, entry.Note))
	}
	return out
}

// apiKeyEntries combines normalized credential order with metadata indexed by
// each credential hash. Missing metadata uses default priority and note values;
// nil configs return nil, and the returned slice can be modified independently
// of policy collections.
//
// Metadata follows credential digests rather than list positions, so reordered
// credentials keep their saved attributes. Each returned entry contains copied
// scalar values and strings, with no mutable map or slice reference into the
// source policy.
func (cfg *ContentModerationConfig) apiKeyEntries() []contentModerationAPIKeyRuntimeEntry {
	if cfg == nil {
		return nil
	}
	keys := cfg.apiKeys()
	metadata := make(map[string]ContentModerationAPIKeyMetadata, len(cfg.APIKeyMetadata))
	for _, item := range normalizeContentModerationAPIKeyMetadata(keys, cfg.APIKeyMetadata) {
		metadata[item.KeyHash] = item
	}
	entries := make([]contentModerationAPIKeyRuntimeEntry, 0, len(keys))
	for _, key := range keys {
		hash := moderationAPIKeyHash(key)
		item := metadata[hash]
		entries = append(entries, contentModerationAPIKeyRuntimeEntry{
			Key:      key,
			KeyHash:  hash,
			Priority: normalizeContentModerationAPIKeyPriority(item.Priority),
			// Retain only the supported note prefix for hash-addressed administrator
			// display.
			Note: trimRunes(strings.TrimSpace(item.Note), maxContentModerationAPIKeyNoteRunes),
		})
	}
	return entries
}

// isAPIKeyFrozen reports whether the hashed credential has a freeze after the
// supplied instant. Blank keys and nil services return false; key health is
// read under its mutex, and equality means the freeze has ended.
func (s *ContentModerationService) isAPIKeyFrozen(key string, now time.Time) bool {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return false
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	return state != nil && state.FrozenUntil.After(now)
}

// preBlockAPIKeyLoadForHash copies one key's status and synchronous load
// counters for display. Missing state returns zero load with its status label;
// average latency divides cumulative milliseconds by completed attempts, and
// status and counters use separate samples.
//
// A completion or health update can occur between those reads, so the returned
// status and counters need not describe the same attempt boundary. The load
// counters themselves are copied together under keyHealthMu, and caller changes
// to the result do not change stored health.
func (s *ContentModerationService) preBlockAPIKeyLoadForHash(index int, hash string, masked string, priority int, note string) ContentModerationAPIKeyLoad {
	load := ContentModerationAPIKeyLoad{
		Index:    index,
		KeyHash:  hash,
		Masked:   masked,
		Status:   "unknown",
		Priority: priority,
		Note:     note,
	}
	status := s.apiKeyStatusForHash(index, hash, masked, true, priority, note)
	load.Status = status.Status
	load.LastLatencyMS = status.LastLatencyMS
	load.LastHTTPStatus = status.LastHTTPStatus
	if hash == "" || s == nil {
		return load
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	if state == nil {
		return load
	}
	load.Active = state.SyncActive
	load.Total = state.SyncTotal
	load.Success = state.SyncSuccess
	load.Errors = state.SyncErrors
	// Integer division reports the completed-attempt mean in whole milliseconds.
	if state.SyncTotal > 0 {
		load.AvgLatencyMS = state.SyncLatencyMS / state.SyncTotal
	}
	return load
}

// apiKeyStatusForHash copies a credential health observation with independent
// time pointers and caller-supplied display metadata. Missing state returns
// unknown. Under keyHealthMu, an active freeze takes precedence over stored
// error, which takes precedence over observed success or attempt completion.
//
// This ordering keeps a prior error visible after freeze expiry until success
// clears it, even when older successful calls exist. Configured records the
// caller-selected origin flag rather than performing a settings lookup, and the
// copied times stay stable after the health lock is released.
func (s *ContentModerationService) apiKeyStatusForHash(index int, hash string, masked string, configured bool, priority int, note string) ContentModerationAPIKeyStatus {
	status := ContentModerationAPIKeyStatus{
		Index:      index,
		KeyHash:    hash,
		Masked:     masked,
		Status:     "unknown",
		Configured: configured,
		Priority:   normalizeContentModerationAPIKeyPriority(priority),
		Note:       note,
	}
	if hash == "" || s == nil {
		return status
	}
	now := time.Now()
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	if state == nil {
		return status
	}
	status.FailureCount = state.FailureCount
	status.SuccessCount = state.SuccessCount
	status.LastError = state.LastError
	status.LastLatencyMS = state.LastLatencyMS
	status.LastHTTPStatus = state.LastHTTPStatus
	status.LastTested = state.LastTested
	if !state.LastCheckedAt.IsZero() {
		t := state.LastCheckedAt
		status.LastCheckedAt = &t
	}
	if state.FrozenUntil.After(now) {
		t := state.FrozenUntil
		status.FrozenUntil = &t
		status.Status = "frozen"
		return status
	}
	if state.LastError != "" {
		status.Status = "error"
		return status
	}
	if state.SuccessCount > 0 || state.LastTested {
		status.Status = "ok"
	}
	return status
}

// contentModerationFreezeDurationForHTTPStatus chooses the key exclusion
// lifetime for an observed status. Transport status zero and HTTP 400 add no
// freeze; authentication, throttling, and other statuses use their separate
// fixed duration classes.
func contentModerationFreezeDurationForHTTPStatus(httpStatus int) time.Duration {
	switch httpStatus {
	case 0, 400:
		return 0
	case 401, 403:
		return contentModerationKeyAuthFreezeDuration
	case 429, 529:
		return contentModerationKeyRateLimitFreezeDuration
	default:
		return contentModerationKeyHTTPErrorFreezeDuration
	}
}

// ensureAPIKeyHealthLocked returns mutable state for a credential hash and
// initializes missing storage. Callers hold keyHealthMu throughout access to
// the result; a nonblank mask replaces its display mask and a blank mask
// retains it.
func (s *ContentModerationService) ensureAPIKeyHealthLocked(hash string, masked string) *contentModerationKeyHealth {
	if s.keyHealth == nil {
		s.keyHealth = make(map[string]*contentModerationKeyHealth)
	}
	state := s.keyHealth[hash]
	if state == nil {
		state = &contentModerationKeyHealth{Hash: hash}
		s.keyHealth[hash] = state
	}
	if strings.TrimSpace(masked) != "" {
		state.Masked = masked
	}
	return state
}

// moderationAPIKeyHash hashes trimmed nonblank credential text to a hexadecimal
// SHA-256 identifier. Blank credentials return empty, allowing callers to
// reject absent health identity without storing or comparing raw credential
// values.
func moderationAPIKeyHash(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
