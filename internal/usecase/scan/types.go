package scan

// ScanRequest is the input for the scan pipeline.
type ScanRequest struct {
	Image string `json:"image" binding:"required"`
	TCG   string `json:"tcg" binding:"required"`
	Lang  string `json:"lang,omitempty"`
	Force bool   `json:"force,omitempty"`
}

// PHashRequest is the input for a perceptual hash lookup.
type PHashRequest struct {
	Image string `json:"image" binding:"required"`
}

// ScanResponse is the output of the scan pipeline.
type ScanResponse struct {
	OK           bool              `json:"ok"`
	Card         *Card             `json:"card,omitempty"`
	Cached       bool              `json:"cached,omitempty"`
	ImageURL     string            `json:"imageUrl,omitempty"`
	Hash         string            `json:"hash,omitempty"`
	PHash        string            `json:"pHash,omitempty"`
	IdentifiedBy string            `json:"identifiedBy,omitempty"`
	CrossCheck   *CrossCheckResult `json:"crossCheck,omitempty"`
	DonVision    *DonIdentification `json:"donVision,omitempty"`
	OCRExtract   *OCROutput        `json:"ocrExtract,omitempty"`
	Preprocess   interface{}       `json:"preprocess,omitempty"`
	Verified     interface{}       `json:"verified,omitempty"`
	HaikuFailed  bool              `json:"haikuFailed,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// CrossCheckResult represents the Haiku vs Vision comparison.
type CrossCheckResult struct {
	Agreed     bool   `json:"agreed"`
	HaikuCode  string `json:"haikuCode"`
	VisionCode string `json:"visionCode"`
	Adopted    string `json:"adopted"`
}

// VerifiedResult wraps a verified card lookup.
type VerifiedResult struct {
	DocKey string                 `json:"docKey"`
	Code   string                 `json:"code"`
	Rarity string                 `json:"rarity"`
	Data   map[string]interface{} `json:"data"`
}
