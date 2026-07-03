package persistence

import "errors"

var (
	errMissingStorage        = errors.New("spec.storage.volumes is required")
	errUnsupportedVolumeMode = errors.New("Slice 1 supports spec.storage.volumes.data.mode Dynamic only")
)
