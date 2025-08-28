package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// PageToken represents the structure of the pagination token.
type PageToken struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// Marshal marshals the PageToken struct into a base64 encoded string.
func (t PageToken) Marshal() (string, error) {
	jsonData, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("failed to marshal page token: %w", err)
	}

	encodedString := base64.URLEncoding.EncodeToString(jsonData)
	return encodedString, nil
}

// UnmarshalPageToken takes a Base64 encoded string and unmarshals it into a
// PageToken.
func UnmarshalPageToken(encodedToken string) (PageToken, error) {
	jsonData, err := base64.URLEncoding.DecodeString(encodedToken)
	if err != nil {
		return PageToken{}, fmt.Errorf("failed to decode base64 token: %w", err)
	}

	var token PageToken
	if err := json.Unmarshal(jsonData, &token); err != nil {
		return PageToken{}, fmt.Errorf("failed to unmarshal page token: %w", err)
	}

	return token, nil
}
