package converter_test

import (
	"testing"
	"time"

	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	converter "github.com/jovulic/zfsilo/app/internal/converter/impl"
	"github.com/jovulic/zfsilo/app/internal/database"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/datatypes"
)

func TestVolumeConversion(t *testing.T) {
	converter := converter.VolumeConverterImpl{}

	createTime := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	updateTime := time.Now().UTC().Truncate(time.Second)

	dbVolume := database.Volume{
		ID:            "vol-12345",
		Name:          "test-volume",
		DatasetID:     "ds-abcde",
		CreateTime:    createTime,
		UpdateTime:    updateTime,
		CapacityBytes: 1073741824,
		Sparse:        true,
		Mode:          database.VolumeModeBLOCK,
		Status:        database.VolumeStatusINITIAL,
		Options: datatypes.NewJSONType(database.VolumeOptionList{
			{Key: "snap", Value: "true"},
			{Key: "atime", Value: "off"},
		}),
		Struct: datatypes.JSON(`{"key":"value","number":123}`),
	}

	expectedAPIVolume := &zfsilov1.Volume{
		Id:            "vol-12345",
		Name:          "test-volume",
		DatasetId:     "ds-abcde",
		CreateTime:    timestamppb.New(createTime),
		UpdateTime:    timestamppb.New(updateTime),
		CapacityBytes: 1073741824,
		Sparse:        true,
		Mode:          zfsilov1.Volume_MODE_BLOCK,
		Status:        zfsilov1.Volume_STATUS_INITIAL,
		Options: []*zfsilov1.Volume_Option{
			{Key: "snap", Value: "true"},
			{Key: "atime", Value: "off"},
		},
		Struct: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"key":    {Kind: &structpb.Value_StringValue{StringValue: "value"}},
				"number": {Kind: &structpb.Value_NumberValue{NumberValue: 123}},
			},
		},
	}

	t.Run("DB to API", func(t *testing.T) {
		// We ignore the generated top-level fields in the API struct for comparison.
		actualAPIVolume, err := converter.FromDBToAPI(dbVolume)
		require.NoError(t, err)

		// Using require stops the test immediately on failure.
		require.Equal(t, expectedAPIVolume.Id, actualAPIVolume.Id)
		require.Equal(t, expectedAPIVolume.Name, actualAPIVolume.Name)
		require.Equal(t, expectedAPIVolume.DatasetId, actualAPIVolume.DatasetId)
		require.Equal(t, expectedAPIVolume.CapacityBytes, actualAPIVolume.CapacityBytes)
		require.Equal(t, expectedAPIVolume.Sparse, actualAPIVolume.Sparse)
		require.Equal(t, expectedAPIVolume.Mode, actualAPIVolume.Mode)
		require.Equal(t, expectedAPIVolume.Status, actualAPIVolume.Status)
		require.True(t, expectedAPIVolume.CreateTime.AsTime().Equal(actualAPIVolume.CreateTime.AsTime()))
		require.True(t, expectedAPIVolume.UpdateTime.AsTime().Equal(actualAPIVolume.UpdateTime.AsTime()))
		require.ElementsMatch(t, expectedAPIVolume.Options, actualAPIVolume.Options)
		require.Equal(t, expectedAPIVolume.Struct.Fields["key"].GetStringValue(), actualAPIVolume.Struct.Fields["key"].GetStringValue())
	})

	t.Run("API to DB", func(t *testing.T) {
		actualDBVolume, err := converter.FromAPIToDB(expectedAPIVolume)
		require.NoError(t, err)

		// Assert that the result matches the original DB object. This ensures the
		// mapping is perfectly symmetrical.
		require.Equal(t, dbVolume.ID, actualDBVolume.ID)
		require.Equal(t, dbVolume.Name, actualDBVolume.Name)
		require.Equal(t, dbVolume.DatasetID, actualDBVolume.DatasetID)
		require.Equal(t, dbVolume.CapacityBytes, actualDBVolume.CapacityBytes)
		require.Equal(t, dbVolume.Sparse, actualDBVolume.Sparse)
		require.Equal(t, dbVolume.Mode, actualDBVolume.Mode)
		require.Equal(t, dbVolume.Status, actualDBVolume.Status)

		// Timestamps can have timezone differences, so comparing them with Equal
		// is best.
		require.True(t, dbVolume.CreateTime.Equal(actualDBVolume.CreateTime))
		require.True(t, dbVolume.UpdateTime.Equal(actualDBVolume.UpdateTime))

		// Compare the JSON content by unmarshalling or comparing the raw bytes.
		require.JSONEq(t, string(dbVolume.Struct), string(actualDBVolume.Struct))
		require.Equal(t, dbVolume.Options, actualDBVolume.Options)
	})
}
