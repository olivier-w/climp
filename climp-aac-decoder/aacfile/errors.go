package aacfile

import (
	"errors"
	"fmt"
)

var (
	ErrUnsupportedBitstream = errors.New("unsupported AAC bitstream")
	ErrMalformedBitstream   = errors.New("malformed AAC bitstream")
	ErrUnvalidatedFeature   = errors.New("unvalidated AAC feature")
)

type UnsupportedFeatureError struct {
	Feature string
	Detail  string
}

func (e *UnsupportedFeatureError) Error() string {
	if e == nil {
		return ErrUnsupportedBitstream.Error()
	}
	if e.Detail == "" {
		return fmt.Sprintf("unsupported AAC feature: %s", e.Feature)
	}
	return fmt.Sprintf("unsupported AAC feature: %s (%s)", e.Feature, e.Detail)
}

func (e *UnsupportedFeatureError) Unwrap() error {
	return ErrUnsupportedBitstream
}

func unsupportedFeature(feature, detail string) error {
	return &UnsupportedFeatureError{Feature: feature, Detail: detail}
}

func malformedf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformedBitstream, fmt.Sprintf(format, args...))
}

func unvalidatedFeature(feature, detail string) error {
	if detail == "" {
		return fmt.Errorf("%w: %s", ErrUnvalidatedFeature, feature)
	}
	return fmt.Errorf("%w: %s (%s)", ErrUnvalidatedFeature, feature, detail)
}
