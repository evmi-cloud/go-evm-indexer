package grpc

import (
	"errors"

	"connectrpc.com/connect"
	"gorm.io/gorm"
)

// dbError maps a metadata-DB error to a Connect code: a missing record becomes
// CodeNotFound instead of the default CodeUnknown, anything else CodeInternal.
func dbError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
