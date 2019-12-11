package config

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetKeystoreConfig(t *testing.T) {
	assert.Panics(t, func(){GetKeystoreConfig(DefaultEnv)})
}
