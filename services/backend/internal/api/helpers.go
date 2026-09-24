package api

import (
	"fmt"
	"time"
)

func validRole(role string) bool                 { return role == "owner" || role == "deployer" || role == "viewer" }
func fmtError(message string, args ...any) error { return fmt.Errorf(message, args...) }
func timeParse(value string) (time.Time, error)  { return time.Parse(time.RFC3339, value) }
