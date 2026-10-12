package qoder

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

func TestQoderConversationStoreExpiresState(t *testing.T) {
	store := NewQoderConversationStore(5 * time.Millisecond)
	messages := []QoderMessage{{Role: "user", Text: "hello"}}

	plan := store.PlanWithOptions("key", "", nil, messages, QoderConversationPlanOptions{})
	require.NotNil(t, plan)
	plan.Commit()

	time.Sleep(10 * time.Millisecond)

	next := store.PlanWithOptions("key", "", nil, messages, QoderConversationPlanOptions{})
	require.False(t, next.Reused)
	require.True(t, next.IncludeSystem)
	require.Len(t, next.MessagesToSend, 1)
}

// TestQoderConversationStoreConcurrent 检查并发提交和回滚后的会话版本。
func TestQoderConversationStoreConcurrent(t *testing.T) {
	store := NewQoderConversationStore(5 * time.Minute)

	const numGoroutines = 50
	const numOpsPerGoroutine = 100

	// 请求共享 key 和 sessionID，各 goroutine 提交相同的 fingerprints。
	key := "test_conversation_key"
	sessionID := "test_session_id"

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// 并发请求会在同一会话中交错读取、提交和回滚。
	for i := range numGoroutines {
		go func(workerID int) {
			defer wg.Done()
			for j := range numOpsPerGoroutine {
				// 每次操作根据当时读取的版本构造计划。
				store.Mu.Lock()
				currentState := store.Items[key]
				store.Mu.Unlock()

				// previousState 是本次提交失败时的恢复目标。
				plan := &QoderConversationPlan{
					Store:             store,
					Key:               key,
					SessionID:         sessionID,
					SystemFingerprint: "system_v1",
					ToolsFingerprint:  "tools_v1",
					PreviousState:     CloneQoderConversationState(currentState),
				}

				// 提交使当前版本递增。
				fingerprints := []string{"msg_1", "msg_2", "msg_3"}
				plan.CommitFingerprints(fingerprints)

				// 偶数编号的 goroutine 每十次操作模拟一次失败回滚。
				if workerID%2 == 0 && j%10 == 0 {
					plan.AcceptedCommitted = true
					plan.RollbackAccepted()
				}
			}
		}(i)
	}

	wg.Wait()

	// 会话经过并发提交后仍应存在。
	store.Mu.Lock()
	finalState := store.Items[key]
	store.Mu.Unlock()
	if finalState == nil {
		t.Error("expected final state to exist")
		return
	}
	if finalState.SessionID != sessionID {
		t.Errorf("expected sessionID=%s, got=%s", sessionID, finalState.SessionID)
	}
	if finalState.Version <= 0 {
		t.Errorf("expected version > 0, got=%d", finalState.Version)
	}
}

// TestQoderConversationRollbackVersionControl 验证 rollback 版本控制防止并发覆盖。
func TestQoderConversationRollbackVersionControl(t *testing.T) {
	store := NewQoderConversationStore(5 * time.Minute)
	key := "test_key"
	sessionID := "session_1"

	// 初始状态：version 1
	plan1 := &QoderConversationPlan{
		Store:             store,
		Key:               key,
		SessionID:         sessionID,
		SystemFingerprint: "sys_v1",
		ToolsFingerprint:  "tools_v1",
	}
	plan1.CommitFingerprints([]string{"msg_1"})

	store.Mu.Lock()
	state1 := store.Items[key]
	store.Mu.Unlock()
	if state1 == nil || state1.Version != 1 {
		t.Fatalf("expected version=1, got=%v", state1)
	}

	// 保存 previousState 用于后续 rollback
	plan1.PreviousState = CloneQoderConversationState(state1)
	plan1.AcceptedState = CloneQoderConversationState(state1)
	plan1.AcceptedCommitted = true

	// 另一个 plan 提交新状态：version 2
	plan2 := &QoderConversationPlan{
		Store:             store,
		Key:               key,
		SessionID:         sessionID,
		SystemFingerprint: "sys_v1",
		ToolsFingerprint:  "tools_v1",
	}
	plan2.CommitFingerprints([]string{"msg_1", "msg_2"})

	store.Mu.Lock()
	state2 := store.Items[key]
	store.Mu.Unlock()
	if state2 == nil || state2.Version != 2 {
		t.Fatalf("expected version=2, got=%v", state2)
	}

	// plan1 尝试 rollback（基于 version 1 的 previousState）
	// 当前 version 已经是 2，回滚 version 1 的计划应被拒绝。
	plan1.RollbackAccepted()

	store.Mu.Lock()
	stateFinal := store.Items[key]
	store.Mu.Unlock()
	if stateFinal == nil {
		t.Fatal("expected state to exist after rollback")
		return
	}
	if stateFinal.Version != 2 {
		t.Errorf("rollback should be rejected, expected version=2, got=%d", stateFinal.Version)
	}
	if len(stateFinal.MessageFingerprints) != 2 {
		t.Errorf("rollback should not modify state, expected 2 messages, got=%d", len(stateFinal.MessageFingerprints))
	}
}

func TestQoderConversationRollbackAcceptedDeletesOwnNewState(t *testing.T) {
	store := NewQoderConversationStore(5 * time.Minute)
	plan := store.PlanWithOptions("rollback_new_state", "system", nil, []QoderMessage{{Role: "user", Text: "hello"}}, QoderConversationPlanOptions{})

	plan.CommitAccepted()

	store.Mu.Lock()
	accepted := CloneQoderConversationState(store.Items[plan.Key])
	store.Mu.Unlock()
	if accepted == nil || accepted.Version != 1 {
		t.Fatalf("expected accepted version=1, got=%v", accepted)
	}

	plan.RollbackAccepted()

	store.Mu.Lock()
	final := store.Items[plan.Key]
	store.Mu.Unlock()
	if final != nil {
		t.Fatalf("expected rollback to delete own new accepted state, got=%v", final)
	}
}

func TestQoderConversationRollbackAcceptedRestoresPreviousState(t *testing.T) {
	store := NewQoderConversationStore(5 * time.Minute)
	key := "rollback_previous_state"
	system := "system"
	firstMessages := []QoderMessage{{Role: "user", Text: "first"}}
	initialPlan := store.PlanWithOptions(key, system, nil, firstMessages, QoderConversationPlanOptions{})
	initialPlan.Commit(upstream.TokenUsage{InputTokens: 10, OutputTokens: 2})

	store.Mu.Lock()
	previous := CloneQoderConversationState(store.Items[key])
	store.Mu.Unlock()
	if previous == nil || previous.Version != 1 || !previous.HasUsage {
		t.Fatalf("unexpected previous state: %#v", previous)
	}

	nextMessages := []QoderMessage{
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "answer"},
		{Role: "user", Text: "next"},
	}
	plan := store.PlanWithOptions(key, system, nil, nextMessages, QoderConversationPlanOptions{})
	if !plan.Reused {
		t.Fatal("expected plan to reuse previous conversation")
	}

	plan.CommitAccepted()

	store.Mu.Lock()
	accepted := CloneQoderConversationState(store.Items[key])
	store.Mu.Unlock()
	if accepted == nil || accepted.Version != 2 {
		t.Fatalf("expected accepted version=2, got=%#v", accepted)
	}

	plan.RollbackAccepted()

	store.Mu.Lock()
	final := CloneQoderConversationState(store.Items[key])
	store.Mu.Unlock()
	if !QoderConversationStateEqual(final, previous) {
		t.Fatalf("expected rollback to restore previous state\nprevious=%#v\nfinal=%#v", previous, final)
	}
}

func TestQoderConversationRollbackAcceptedDoesNotClobberConcurrentCommit(t *testing.T) {
	store := NewQoderConversationStore(5 * time.Minute)
	key := "rollback_concurrent_commit"
	system := "system"
	firstMessages := []QoderMessage{{Role: "user", Text: "first"}}
	initialPlan := store.PlanWithOptions(key, system, nil, firstMessages, QoderConversationPlanOptions{})
	initialPlan.Commit()

	plan := store.PlanWithOptions(key, system, nil, []QoderMessage{
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "answer"},
		{Role: "user", Text: "next"},
	}, QoderConversationPlanOptions{})
	plan.CommitAccepted()

	concurrentPlan := store.PlanWithOptions(key, system, nil, []QoderMessage{
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "answer"},
		{Role: "user", Text: "next"},
	}, QoderConversationPlanOptions{})
	concurrentPlan.Commit()

	store.Mu.Lock()
	concurrentState := CloneQoderConversationState(store.Items[key])
	store.Mu.Unlock()
	if concurrentState == nil || concurrentState.Version != 3 {
		t.Fatalf("expected concurrent version=3, got=%#v", concurrentState)
	}

	plan.RollbackAccepted()

	store.Mu.Lock()
	final := CloneQoderConversationState(store.Items[key])
	store.Mu.Unlock()
	if !QoderConversationStateEqual(final, concurrentState) {
		t.Fatalf("rollback clobbered concurrent commit\nconcurrent=%#v\nfinal=%#v", concurrentState, final)
	}
}
