package installer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tojiuni/morphso/internal/installer"
)

func TestBuildCommand_NativePip(t *testing.T) {
	cmd := installer.BuildCommand("pip", "native", "gopedia", "1.0.0")
	assert.Equal(t, []string{"pip", "install", "gopedia==1.0.0"}, cmd)
}

func TestBuildCommand_NativePipNoVersion(t *testing.T) {
	cmd := installer.BuildCommand("pip", "native", "gopedia", "")
	assert.Equal(t, []string{"pip", "install", "gopedia"}, cmd)
}

func TestBuildCommand_NativeNpm(t *testing.T) {
	cmd := installer.BuildCommand("npm", "native", "gopedia", "2.0.0")
	assert.Equal(t, []string{"npm", "install", "-g", "gopedia@2.0.0"}, cmd)
}

func TestBuildCommand_NativeMcp(t *testing.T) {
	// mcp 타입은 pip로 처리
	cmd := installer.BuildCommand("mcp", "native", "gopedia", "1.0.0")
	assert.Equal(t, []string{"pip", "install", "gopedia==1.0.0"}, cmd)
}

func TestBuildCommand_Docker(t *testing.T) {
	cmd := installer.BuildCommand("mcp", "docker", "gopedia", "1.0.0")
	assert.Equal(t, []string{
		"docker", "run", "-d", "--name", "gopedia",
		"artifacts.toji.homes/gopedia:1.0.0",
	}, cmd)
}

func TestBuildCommand_DockerLatest(t *testing.T) {
	cmd := installer.BuildCommand("mcp", "docker", "gopedia", "")
	assert.Equal(t, []string{
		"docker", "run", "-d", "--name", "gopedia",
		"artifacts.toji.homes/gopedia:latest",
	}, cmd)
}

func TestBuildCommand_Helm(t *testing.T) {
	cmd := installer.BuildCommand("helm", "helm", "gopedia", "1.0.0")
	assert.Equal(t, []string{
		"helm", "install", "gopedia", "morphso/gopedia", "--version", "1.0.0",
	}, cmd)
}

func TestBuildCommand_K8s(t *testing.T) {
	cmd := installer.BuildCommand("mcp", "k8s", "gopedia", "1.0.0")
	assert.Equal(t, []string{
		"helm", "install", "gopedia", "morphso/gopedia", "--version", "1.0.0",
	}, cmd)
}

func TestCheckPrerequisite(t *testing.T) {
	// "go"는 테스트 환경에 존재
	assert.True(t, installer.CheckPrerequisite("go"))
	assert.False(t, installer.CheckPrerequisite("__nonexistent_tool_xyz__"))
}
