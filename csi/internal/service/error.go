package service

import (
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// isErrorID returns true if the error is an InvalidArgument error specifically
// related to a malformed ID.
func isErrorID(err error) bool {
	return connect.CodeOf(err) == connect.CodeInvalidArgument && strings.Contains(err.Error(), "id")
}

// mapError translates backend connect errors into gRPC status errors suitable
// for the CSI driver.
func mapError(err error) error {
	if err == nil {
		return nil
	}

	code := connect.CodeOf(err)
	msg := err.Error()

	// Special handling for busy datasets which should be FailedPrecondition in CSI.
	if strings.Contains(msg, "dataset is busy") {
		return status.Error(codes.FailedPrecondition, msg)
	}

	switch code {
	case connect.CodeNotFound:
		return status.Error(codes.NotFound, msg)
	case connect.CodeAlreadyExists:
		return status.Error(codes.AlreadyExists, msg)
	case connect.CodeInvalidArgument:
		return status.Error(codes.InvalidArgument, msg)
	case connect.CodeFailedPrecondition:
		return status.Error(codes.FailedPrecondition, msg)
	case connect.CodePermissionDenied:
		return status.Error(codes.PermissionDenied, msg)
	case connect.CodeUnauthenticated:
		return status.Error(codes.Unauthenticated, msg)
	default:
		return status.Error(codes.Internal, msg)
	}
}

// mapErrorID maps an error to NotFound if it is an ID validation error,
// otherwise uses standard mapping.
func mapErrorID(err error) error {
	if isErrorID(err) {
		return status.Error(codes.NotFound, "volume id not found (invalid format)")
	}
	return mapError(err)
}
