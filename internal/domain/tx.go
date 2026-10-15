package domain

import "context"

// Tx is an opaque transaction handle. Kept as `any` so the domain package stays free of any
// database driver dependency; only the repository layer type-asserts it back.
type Tx = any

// TxManager runs a unit of work in a single database transaction.
//
// Lets the service compose several repository writes atomically (e.g. create a kid and replace its
// subjects) without importing SQL drivers. Implementations begin, invoke fn, then commit on a nil
// error and roll back otherwise.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(tx Tx) error) error
}
