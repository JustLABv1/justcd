package store

import (
	"encoding/json"
	"github.com/justlab/justcd/services/backend/internal/core"
)

// Historical operations can contain {} or completed:null. Always return an
// array so clients receive the same progress shape for every operation type.
func (p OperationProgress) MarshalJSON() ([]byte, error) {
	type progress OperationProgress
	if p.Completed == nil {
		p.Completed = []core.Identity{}
	}
	return json.Marshal(progress(p))
}
