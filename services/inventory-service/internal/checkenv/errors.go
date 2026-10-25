package checkenv

import "errors"

var errCreatePool = errors.New("postgres: error creating pool")

var errPingDB = errors.New("postgres: error ping database")

var errEmptyAppName = errors.New("postgres: error empty app name")
