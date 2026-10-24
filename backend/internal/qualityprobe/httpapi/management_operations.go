package httpapi

import (
	"github.com/TokenFlux/TokenRouter/internal/qualityprobe"
	"github.com/TokenFlux/TokenRouter/internal/server/adminapi"
)

// management_operations.go describes quality-probe administrator endpoints for
// the management catalogue. Route registration reuses these operations so MCP
// discovery, authorization, auditing, and the HTTP router share each action.

// ManagementOperations supplies quality-probe route descriptions for administrators.
var ManagementOperations = map[string]*adminapi.Operation{
	"quality_probe.settings.get": adminapi.Define(adminapi.Operation{
		ID:          "quality_probe.settings.get",
		Module:      "quality-probe",
		Title:       "Get Quality Probe Settings",
		Description: "Read the administrator configuration that controls automatic OpenAI quality probes.",
		Method:      "GET",
		Path:        "/api/v1/admin/quality-probe/settings",
		ReadOnly:    true,
		Response:    adminapi.ResponseJSON,
		BrowserPath: "/admin/settings",
		Input:       adminapi.Arguments("/api/v1/admin/quality-probe/settings", nil, nil),
	}),
	"quality_probe.settings.update": adminapi.Define(adminapi.Operation{
		ID:          "quality_probe.settings.update",
		Module:      "quality-probe",
		Title:       "Update Quality Probe Settings",
		Description: "Save automatic OpenAI quality-probe settings, including model, timing, groups, and failure handling.",
		Method:      "PUT",
		Path:        "/api/v1/admin/quality-probe/settings",
		Sensitive:   true,
		Response:    adminapi.ResponseJSON,
		BrowserPath: "/admin/settings",
		Input:       adminapi.Arguments("/api/v1/admin/quality-probe/settings", nil, (*qualityprobe.SettingsPayload)(nil)),
	}),
	"quality_probe.logs.list": adminapi.Define(adminapi.Operation{
		ID:          "quality_probe.logs.list",
		Module:      "quality-probe",
		Title:       "List Quality Probe Logs",
		Description: "Read paginated automatic and manual quality-probe records stored with OpenAI providers.",
		Method:      "GET",
		Path:        "/api/v1/admin/quality-probe/logs",
		ReadOnly:    true,
		Response:    adminapi.ResponseJSON,
		BrowserPath: "/admin/quality-probe",
		Input:       adminapi.Arguments("/api/v1/admin/quality-probe/logs", []string{"page", "page_size"}, nil),
	}),
	"quality_probe.provider.get": adminapi.Define(adminapi.Operation{
		ID:          "quality_probe.provider.get",
		Module:      "quality-probe",
		Title:       "Get Provider Quality Probe State",
		Description: "Read one provider quality-probe state and recent recorded results.",
		Method:      "GET",
		Path:        "/api/v1/admin/providers/:id/quality-probe",
		ReadOnly:    true,
		Response:    adminapi.ResponseJSON,
		BrowserPath: "/admin/providers",
		Input:       adminapi.Arguments("/api/v1/admin/providers/:id/quality-probe", nil, nil),
	}),
	"quality_probe.provider.run": adminapi.Define(adminapi.Operation{
		ID:          "quality_probe.provider.run",
		Module:      "quality-probe",
		Title:       "Run Provider Quality Probe",
		Description: "Run the candy and fingerprint probes against one OpenAI provider using the optional requested model.",
		Method:      "POST",
		Path:        "/api/v1/admin/providers/:id/quality-probe",
		Sensitive:   true,
		Response:    adminapi.ResponseJSON,
		BrowserPath: "/admin/providers",
		Input:       adminapi.Arguments("/api/v1/admin/providers/:id/quality-probe", nil, map[string]string{}),
	}),
}
