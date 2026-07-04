package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const apiEndpoint = "https://vision.googleapis.com/v1/images:annotate"

// Feature represents a Vision API feature request.
type Feature struct {
	Type       string `json:"type"`
	MaxResults int    `json:"maxResults,omitempty"`
}

// ImageSource represents a remote image URI.
type ImageSource struct {
	ImageURI string `json:"imageUri"`
}

// Image represents the image payload.
type Image struct {
	Source  *ImageSource `json:"source,omitempty"`
	Content string       `json:"content,omitempty"`
}

// AnnotateRequest is a single annotation request.
type AnnotateRequest struct {
	Image    Image     `json:"image"`
	Features []Feature `json:"features"`
}

// BatchRequest is the top-level request body.
type BatchRequest struct {
	Requests []AnnotateRequest `json:"requests"`
}

// WebDetection mirrors the relevant Vision web detection result.
type WebDetection struct {
	BestGuessLabels []struct {
		Label string `json:"label"`
	} `json:"bestGuessLabels"`
	WebEntities []struct {
		EntityID    string  `json:"entityId"`
		Score       float64 `json:"score"`
		Description string  `json:"description"`
	} `json:"webEntities"`
	FullMatchingImages []struct {
		URL string `json:"url"`
	} `json:"fullMatchingImages"`
	PartialMatchingImages []struct {
		URL string `json:"url"`
	} `json:"partialMatchingImages"`
	PagesWithMatchingImages []struct {
		URL                   string `json:"url"`
		Score                 float64 `json:"score"`
		PageTitle             string `json:"pageTitle"`
		FullMatchingImages    []struct {
			URL string `json:"url"`
		} `json:"fullMatchingImages"`
		PartialMatchingImages []struct {
			URL string `json:"url"`
		} `json:"partialMatchingImages"`
	} `json:"pagesWithMatchingImages"`
	VisuallySimilarImages []struct {
		URL string `json:"url"`
	} `json:"visuallySimilarImages"`
}

// AnnotateResponse is a single annotation response.
type AnnotateResponse struct {
	WebDetection       *WebDetection `json:"webDetection"`
	FullTextAnnotation *struct {
		Text string `json:"text"`
	} `json:"fullTextAnnotation"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// BatchResponse is the top-level response body.
type BatchResponse struct {
	Responses []AnnotateResponse `json:"responses"`
}

// Result is the simplified output of a web+OCR call.
type Result struct {
	OK      bool          `json:"ok"`
	Web     *WebDetection `json:"web"`
	OCRText string        `json:"ocrText"`
	Error   string        `json:"error,omitempty"`
}

// Client is a Google Vision API REST client.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new Vision client.
func NewClient(apiKey string) *Client {
	if apiKey == "" {
		return nil
	}
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WebDetect performs WEB_DETECTION + DOCUMENT_TEXT_DETECTION on an image.
func (c *Client) WebDetect(ctx context.Context, imageURL, imageBase64 string, maxResults int) (*Result, error) {
	if c == nil {
		return nil, fmt.Errorf("vision client not initialized")
	}
	if maxResults <= 0 {
		maxResults = 50
	}

	var img Image
	if imageBase64 != "" {
		img.Content = imageBase64
	} else if imageURL != "" {
		img.Source = &ImageSource{ImageURI: imageURL}
	} else {
		return nil, fmt.Errorf("imageURL or imageBase64 required")
	}

	reqBody := BatchRequest{
		Requests: []AnnotateRequest{{
			Image: img,
			Features: []Feature{
				{Type: "WEB_DETECTION", MaxResults: maxResults},
				{Type: "DOCUMENT_TEXT_DETECTION"},
			},
		}},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s?key=%s", apiEndpoint, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	var batchResp BatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(batchResp.Responses) == 0 {
		return &Result{OK: false, Error: "empty vision response"}, nil
	}
	ar := batchResp.Responses[0]
	if ar.Error != nil {
		return &Result{OK: false, Error: ar.Error.Message}, nil
	}

	result := &Result{OK: true, Web: ar.WebDetection}
	if ar.FullTextAnnotation != nil {
		result.OCRText = ar.FullTextAnnotation.Text
	}
	return result, nil
}
