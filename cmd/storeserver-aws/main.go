package main

import (
	"upspin.io/cloud/https"
	"upspin.io/serverutil/storeserver"

	// Storage implementation.
	_ "aws.upspin.io/cloud/storage/s3"
	_ "upspin.io/cloud/storage/disk"
)

func main() {
	ready := storeserver.Main()
	https.ListenAndServeFromFlags(ready)
}
