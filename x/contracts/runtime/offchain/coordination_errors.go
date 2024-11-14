package offchain

import "errors"

var (
    ErrNotEnoughWorkers     = errors.New("not enough available workers")
    ErrCoordinationTimeout  = errors.New("coordination timeout")
    ErrInvalidWorkerIndex   = errors.New("invalid worker index")
    ErrCoordinationFailed   = errors.New("coordination failed")
)