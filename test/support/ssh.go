// Package support provides test helpers for SSH execution and assertions.
package support

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHClient wraps an SSH connection for test command execution.
type SSHClient struct {
	conn   *ssh.Client
	Host   string
	User   string
}

// SSHResult holds the result of a remote command execution.
type SSHResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Connect establishes an SSH connection with key-based authentication.
func Connect(host, user, keyPath string) (*SSHClient, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading SSH key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("parsing SSH key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	conn, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), config)
	if err != nil {
		return nil, fmt.Errorf("SSH dial: %w", err)
	}

	return &SSHClient{conn: conn, Host: host, User: user}, nil
}

// ConnectWithRetry tries to connect via SSH with retry logic for boot-time availability.
func ConnectWithRetry(host, user, keyPath string, maxRetries int, interval time.Duration) (*SSHClient, error) {
	var lastErr error
	for i := range maxRetries {
		client, err := Connect(host, user, keyPath)
		if err == nil {
			return client, nil
		}
		lastErr = err
		if i < maxRetries-1 {
			time.Sleep(interval)
		}
	}
	return nil, fmt.Errorf("SSH connect failed after %d retries: %w", maxRetries, lastErr)
}

// Run executes a command remotely and returns the result.
func (c *SSHClient) Run(cmd string) (*SSHResult, error) {
	session, err := c.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	result := &SSHResult{}
	err = session.Run(cmd)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			result.ExitCode = exitErr.ExitStatus()
		} else {
			return result, fmt.Errorf("running command: %w", err)
		}
	}

	return result, nil
}

// Upload transfers a local file to a remote path via SCP-like mechanism.
func (c *SSHClient) Upload(localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("reading local file: %w", err)
	}

	return c.WriteFile(remotePath, data, 0644)
}

// WriteFile writes content to a remote file.
func (c *SSHClient) WriteFile(remotePath string, content []byte, mode os.FileMode) error {
	session, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	// Use cat to write — simpler than SCP protocol
	session.Stdin = bytes.NewReader(content)
	cmd := fmt.Sprintf("cat > %s && chmod %o %s", remotePath, mode, remotePath)
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("writing remote file: %w", err)
	}

	return nil
}

// Download reads a remote file and returns its contents.
func (c *SSHClient) Download(remotePath string) ([]byte, error) {
	session, err := c.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	session.Stdout = &stdout

	if err := session.Run("cat " + remotePath); err != nil {
		return nil, fmt.Errorf("reading remote file: %w", err)
	}

	return io.ReadAll(&stdout)
}

// Close closes the SSH connection.
func (c *SSHClient) Close() error {
	return c.conn.Close()
}
