package middleware

import (
	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
)

func googleTeamAPIKeyError(err error) (int, string, bool) { return keyhttp.GoogleTeamError(err) }
