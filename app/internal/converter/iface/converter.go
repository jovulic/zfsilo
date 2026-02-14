// Package converteriface contains the interface type definitions used by the
// goverter library.
package converteriface

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/datatypes"
)

func ConvertFromJSONToStruct(source datatypes.JSON) (*structpb.Struct, error) {
	destination := &structpb.Struct{}
	if err := destination.UnmarshalJSON(source); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %T to %T: %w", source, destination, err)
	}
	return destination, nil
}

func ConvertFromStructToJSON(source *structpb.Struct) (datatypes.JSON, error) {
	json, err := source.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %T: %w", source, err)
	}
	return json, nil
}

func ConvertTimeToTimestamp(source time.Time) (*timestamppb.Timestamp, error) {
	if source.IsZero() {
		return nil, nil
	}
	destination := timestamppb.New(source)
	if err := destination.CheckValid(); err != nil {
		return nil, fmt.Errorf("failed to convert %T to %T: %w", source, destination, err)
	}
	return destination, nil
}

func ConvertTimestampToTime(source *timestamppb.Timestamp) (time.Time, error) {
	if source == nil {
		return time.Time{}, nil
	}
	if err := source.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("invalid %T: %w", source, err)
	}
	return source.AsTime(), nil
}
