package service

func toVolumeID(name string) string {
	return "vol_" + name
}

func toVolumeName(name string) string {
	return "volumes/" + toVolumeID(name)
}

func toDatasetID(name string, parentDatasetID string) string {
	return parentDatasetID + "/" + toVolumeID(name)
}
