package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
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

// ParseResourceName extracts all resource IDs from a resource name. It assumes
// the path follows a repeating "name/id/name/id..." pattern.
func ParseResourceName(resourceName string) ([]string, error) {
	// Return an error if the path is empty.
	if resourceName == "" {
		return nil, fmt.Errorf("resource name cannot be empty")
	}

	// Split the path into its components.
	parts := strings.Split(resourceName, "/")

	// A valid path must have an even number of parts (a name for every id).
	if len(parts)%2 != 0 {
		return nil, fmt.Errorf("invalid path format: path must contain pairs of resourceName/resourceId")
	}

	// Pre-allocate a slice with the exact capacity needed.
	ids := make([]string, 0, len(parts)/2)

	// Loop through the parts, incrementing by 2 to only access the ID elements.
	// The IDs are at indices 1, 3, 5, etc.
	for i := 1; i < len(parts); i += 2 {
		id := parts[i]
		// Ensure the ID itself is not an empty string.
		if id == "" {
			pairIndex := (i / 2) + 1
			return nil, fmt.Errorf("resource id in pair %d of resource name %s was empty", pairIndex, resourceName)
		}
		ids = append(ids, id)
	}

	return ids, nil
}

