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

// mapError translates backend connect errors and local errors into gRPC status
// errors suitable for the CSI driver.
func mapError(err error) error {
	if err == nil {
		return nil
	}

	// If it's already a gRPC status error (e.g., from our local validation),
	// return it as-is.
	if _, ok := status.FromError(err); ok {
		return err
	}

	// If it's a Connect error from the zfsilo app, map its code.
	code := connect.CodeOf(err)
	if code != connect.CodeUnknown {
		return status.Error(mapConnectCodeToGRPC(code), err.Error())
	}

	// For all other local errors (e.g., os/exec failures), return Internal.
	return status.Error(codes.Internal, err.Error())
}

// mapConnectCodeToGRPC maps a connect.Code to a gRPC codes.Code.
func mapConnectCodeToGRPC(code connect.Code) codes.Code {
	//nolint:exhaustive
	switch code {
	case connect.CodeCanceled:
		return codes.Canceled
	case connect.CodeUnknown:
		return codes.Unknown
	case connect.CodeInvalidArgument:
		return codes.InvalidArgument
	case connect.CodeDeadlineExceeded:
		return codes.DeadlineExceeded
	case connect.CodeNotFound:
		return codes.NotFound
	case connect.CodeAlreadyExists:
		return codes.AlreadyExists
	case connect.CodePermissionDenied:
		return codes.PermissionDenied
	case connect.CodeResourceExhausted:
		return codes.ResourceExhausted
	case connect.CodeFailedPrecondition:
		return codes.FailedPrecondition
	case connect.CodeAborted:
		return codes.Aborted
	case connect.CodeOutOfRange:
		return codes.OutOfRange
	case connect.CodeUnimplemented:
		return codes.Unimplemented
	case connect.CodeInternal:
		return codes.Internal
	case connect.CodeUnavailable:
		return codes.Unavailable
	case connect.CodeDataLoss:
		return codes.DataLoss
	case connect.CodeUnauthenticated:
		return codes.Unauthenticated
	default:
		return codes.Internal
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
