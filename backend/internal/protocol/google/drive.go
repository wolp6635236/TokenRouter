package google

// DriveStorageInfo represents Google Drive storage quota information.
type DriveStorageInfo struct {
	Limit int64 `json:"limit"` // Storage limit in bytes
	Usage int64 `json:"usage"` // Current usage in bytes
}
