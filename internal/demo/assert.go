package demo

import (
	"github.com/doodla/plundrio/internal/download"
	"github.com/doodla/plundrio/internal/server"
)

// Compile-time proof that the demo Client satisfies BOTH runtime put.io
// interfaces. If either interface gains or changes a method, this file fails to
// compile — the cheapest possible regression guard for "the demo binary still
// boots in both the RPC server and the download manager."
var (
	_ server.PutioClient   = (*Client)(nil)
	_ download.PutioClient = (*Client)(nil)
)
