package config

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetFilesystemConfig_Default(t *testing.T) {
	actual := GetFilesystemConfig()
	expected := FilesystemConfig{
		MGSDevice: "sdb",
		MaxMDTs: uint(24),
		HostGroup: "dac-prod",
		AnsibleDir: "/var/lib/data-acc/fs-ansible/",
		SkipAnsible: false,
		LnetSuffix: "",
		MDTSizeMB: uint(20 * 1024),
	}
	assert.Equal(t, expected, actual)
}
