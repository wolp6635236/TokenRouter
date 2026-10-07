package content

import "github.com/TokenFlux/TokenRouter/internal/pkg/locale"

// LoginAgreementCopy 将协议标题和正文作为完整语言版本。
type LoginAgreementCopy struct {
	Title     string `json:"title"`
	ContentMD string `json:"content_md"`
}

type LoginAgreementDocument struct {
	Resolution   *locale.Resolution                 `json:"localization_resolution,omitempty"`
	Localization *locale.Update[LoginAgreementCopy] `json:"localization,omitempty"`
	ID           string                             `json:"id"`
	Title        string                             `json:"title"`
	ContentMD    string                             `json:"content_md"`
}
