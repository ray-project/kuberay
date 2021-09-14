package util

import (
	"github.com/golang/glog"
	"github.com/google/uuid"
)

type UUIDGeneratorInterface interface {
	NewRandom() (uuid.UUID, error)
}

// UUIDGenerator is the concrete implementation of the UUIDGeneratorInterface used to
// generate UUIDs in production deployments.
type UUIDGenerator struct {
}

func NewUUIDGenerator() *UUIDGenerator {
	return &UUIDGenerator{}
}

func (r *UUIDGenerator) NewRandom() (uuid.UUID, error) {
	return uuid.NewRandom()
}

// FakeUUIDGenerator is a fake implementation of the UUIDGeneratorInterface used for testing.
// It always generates the UUID and error provided during instantiation.
type FakeUUIDGenerator struct {
	uuidToReturn uuid.UUID
	errToReturn  error
}

// NewFakeUUIDGeneratorOrFatal creates a UUIDGenerator that always returns the UUID and error
// provided as parameters.
func NewFakeUUIDGeneratorOrFatal(uuidStringToReturn string, errToReturn error) UUIDGeneratorInterface {
	uuidToReturn, err := uuid.Parse(uuidStringToReturn)
	if err != nil {
		glog.Fatalf("Could not parse the UUID %v: %+v", uuidStringToReturn, err)
	}
	return &FakeUUIDGenerator{
		uuidToReturn: uuidToReturn,
		errToReturn:  errToReturn,
	}
}

func (f *FakeUUIDGenerator) NewRandom() (uuid.UUID, error) {
	return f.uuidToReturn, f.errToReturn
}
