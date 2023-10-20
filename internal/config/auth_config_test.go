package config

import (
	"errors"
	"fmt"
	"testing"

	ghConfig "github.com/cli/go-gh/v2/pkg/config"
	"github.com/cli/go-gh/v2/pkg/config/migration"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

func newTestAuthConfig(t *testing.T) *AuthConfig {
	authCfg := AuthConfig{
		cfg: ghConfig.ReadFromString(""),
	}

	// The real implementation of config.Read uses a sync.Once
	// to read config files and initialise package level variables
	// that are used from then on.
	//
	// This means that tests can't be isolated from each other, so
	// we swap out the function here to return a new config each time.
	ghConfig.Read = func(_ *ghConfig.Config) (*ghConfig.Config, error) {
		return authCfg.cfg, nil
	}

	// The config.Write method isn't defined in the same way as Read to allow
	// the function to be swapped out and it does try to write to disk.
	//
	// We should consider whether it makes sense to change that but in the meantime
	// we can use GH_CONFIG_DIR env var to ensure the tests remain isolated.
	StubWriteConfig(t)

	return &authCfg
}

func TestTokenFromKeyring(t *testing.T) {
	// Given a keyring that contains a token for a host
	keyring.MockInit()
	require.NoError(t, keyring.Set(keyringServiceName("github.com"), "", "test-token"))

	// When we get the token from the auth config
	authCfg := newTestAuthConfig(t)
	token, err := authCfg.TokenFromKeyring("github.com")

	// Then it returns successfully with the correct token
	require.NoError(t, err)
	require.Equal(t, "test-token", token)
}

func TestTokenStoredInConfig(t *testing.T) {
	// When the user has logged in insecurely
	authCfg := newTestAuthConfig(t)
	_, err := authCfg.Login("github.com", "test-user", "test-token", "", false)
	require.NoError(t, err)

	// When we get the token
	token, source := authCfg.Token("github.com")

	// Then the token is successfully fetched
	// and the source is set to oauth_token but this isn't great:
	// https://github.com/cli/go-gh/issues/94
	require.Equal(t, "test-token", token)
	require.Equal(t, "oauth_token", source)
}

func TestTokenStoredInEnv(t *testing.T) {
	// When the user is authenticated via env var
	authCfg := newTestAuthConfig(t)
	t.Setenv("GH_TOKEN", "test-token")

	// When we get the token
	token, source := authCfg.Token("github.com")

	// Then the token is successfully fetched
	// and the source is set to the name of the env var
	require.Equal(t, "test-token", token)
	require.Equal(t, "GH_TOKEN", source)
}

func TestTokenStoredInKeyring(t *testing.T) {
	// When the user has logged in securely
	keyring.MockInit()
	authCfg := newTestAuthConfig(t)
	_, err := authCfg.Login("github.com", "test-user", "test-token", "", true)
	require.NoError(t, err)

	// When we get the token
	token, source := authCfg.Token("github.com")

	// Then the token is successfully fetched
	// and the source is set to keyring
	require.Equal(t, "test-token", token)
	require.Equal(t, "keyring", source)
}

func TestTokenFromKeyringNonExistent(t *testing.T) {
	// Given a keyring that doesn't contain any tokens
	keyring.MockInit()

	// When we try to get a token from the auth config
	authCfg := newTestAuthConfig(t)
	_, err := authCfg.TokenFromKeyring("github.com")

	// Then it returns failure bubbling the ErrNotFound
	require.ErrorIs(t, err, keyring.ErrNotFound)
}

func TestHasEnvTokenWithoutAnyEnvToken(t *testing.T) {
	// Given we have no env set
	authCfg := newTestAuthConfig(t)

	// When we check if it has an env token
	hasEnvToken := authCfg.HasEnvToken()

	// Then it returns false
	require.False(t, hasEnvToken, "expected not to have env token")
}

func TestHasEnvTokenWithEnvToken(t *testing.T) {
	// Given we have an env token set
	// Note that any valid env var for tokens will do, not just GH_ENTERPRISE_TOKEN
	authCfg := newTestAuthConfig(t)
	t.Setenv("GH_ENTERPRISE_TOKEN", "test-token")

	// When we check if it has an env token
	hasEnvToken := authCfg.HasEnvToken()

	// Then it returns true
	require.True(t, hasEnvToken, "expected to have env token")
}

func TestHasEnvTokenWithNoEnvTokenButAConfigVar(t *testing.T) {
	t.Skip("this test is explicitly breaking some implementation assumptions")

	// Given a token in the config
	authCfg := newTestAuthConfig(t)
	// Using example.com here will cause the token to be returned from the config
	_, err := authCfg.Login("example.com", "test-user", "test-token", "", false)
	require.NoError(t, err)

	// When we check if it has an env token
	hasEnvToken := authCfg.HasEnvToken()

	// Then it SHOULD return false
	require.False(t, hasEnvToken, "expected not to have env token")
}

func TestUserNotLoggedIn(t *testing.T) {
	// Given we have not logged in
	authCfg := newTestAuthConfig(t)

	// When we get the user
	_, err := authCfg.User("github.com")

	// Then it returns failure, bubbling the KeyNotFoundError
	var keyNotFoundError *ghConfig.KeyNotFoundError
	require.ErrorAs(t, err, &keyNotFoundError)
}

func TestGitProtocolNotLoggedInDefaults(t *testing.T) {
	// Given we have not logged in
	authCfg := newTestAuthConfig(t)

	// When we get the git protocol
	gitProtocol := authCfg.GitProtocol("github.com")

	// Then it returns the default
	require.Equal(t, "https", gitProtocol)
}

func TestHostsIncludesEnvVar(t *testing.T) {
	// Given the GH_HOST env var is set
	authCfg := newTestAuthConfig(t)
	t.Setenv("GH_HOST", "ghe.io")

	// When we get the hosts
	hosts := authCfg.Hosts()

	// Then the host in the env var is included
	require.Contains(t, hosts, "ghe.io")
}

func TestDefaultHostFromEnvVar(t *testing.T) {
	// Given the GH_HOST env var is set
	authCfg := newTestAuthConfig(t)
	t.Setenv("GH_HOST", "ghe.io")

	// When we get the DefaultHost
	defaultHost, source := authCfg.DefaultHost()

	// Then the returned host and source are using the env var
	require.Equal(t, "ghe.io", defaultHost)
	require.Equal(t, "GH_HOST", source)
}

func TestDefaultHostNotLoggedIn(t *testing.T) {
	// Given we are not logged in
	authCfg := newTestAuthConfig(t)

	// When we get the DefaultHost
	defaultHost, source := authCfg.DefaultHost()

	// Then the returned host is always github.com
	require.Equal(t, "github.com", defaultHost)
	require.Equal(t, "default", source)
}

func TestDefaultHostLoggedInToOnlyOneHost(t *testing.T) {
	// Given we are logged into one host (not github.com to differentiate from the fallback)
	authCfg := newTestAuthConfig(t)
	_, err := authCfg.Login("ghe.io", "test-user", "test-token", "", false)
	require.NoError(t, err)

	// When we get the DefaultHost
	defaultHost, source := authCfg.DefaultHost()

	// Then the returned host is that logged in host and the source is the hosts config
	require.Equal(t, "ghe.io", defaultHost)
	require.Equal(t, "hosts", source)
}

func TestLoginSecureStorageUsesKeyring(t *testing.T) {
	// Given a usable keyring
	keyring.MockInit()
	authCfg := newTestAuthConfig(t)

	// When we login with secure storage
	insecureStorageUsed, err := authCfg.Login("github.com", "test-user", "test-token", "", true)

	// Then it returns success, notes that insecure storage was not used, and stores the token in the keyring
	require.NoError(t, err)
	require.False(t, insecureStorageUsed, "expected to use secure storage")

	token, err := keyring.Get(keyringServiceName("github.com"), "")
	require.NoError(t, err)
	require.Equal(t, "test-token", token)
}

func TestLoginSecureStorageRemovesOldInsecureConfigToken(t *testing.T) {
	// Given a usable keyring and an oauth token in the config
	keyring.MockInit()
	authCfg := newTestAuthConfig(t)
	authCfg.cfg.Set([]string{hosts, "github.com", oauthToken}, "old-token")

	// When we login with secure storage
	_, err := authCfg.Login("github.com", "test-user", "test-token", "", true)

	// Then it returns success, having also removed the old token from the config
	require.NoError(t, err)
	requireNoKey(t, authCfg.cfg, []string{hosts, "github.com", oauthToken})
}

func TestLoginSecureStorageWithErrorFallsbackAndReports(t *testing.T) {
	// Given a keyring that errors
	keyring.MockInitWithError(errors.New("test-explosion"))
	authCfg := newTestAuthConfig(t)

	// When we login with secure storage
	insecureStorageUsed, err := authCfg.Login("github.com", "test-user", "test-token", "", true)

	// Then it returns success, reports that insecure storage was used, and stores the token in the config
	require.NoError(t, err)

	require.True(t, insecureStorageUsed, "expected to use insecure storage")
	requireKeyWithValue(t, authCfg.cfg, []string{hosts, "github.com", oauthToken}, "test-token")
}

func TestLoginInsecureStorage(t *testing.T) {
	// Given we are not logged in
	authCfg := newTestAuthConfig(t)

	// When we login with insecure storage
	insecureStorageUsed, err := authCfg.Login("github.com", "test-user", "test-token", "", false)

	// Then it returns success, notes that insecure storage was used, and stores the token in the config
	require.NoError(t, err)

	require.True(t, insecureStorageUsed, "expected to use insecure storage")
	requireKeyWithValue(t, authCfg.cfg, []string{hosts, "github.com", oauthToken}, "test-token")
}

func TestLoginSetsUserForProvidedHost(t *testing.T) {
	// Given we are not logged in
	authCfg := newTestAuthConfig(t)

	// When we login
	_, err := authCfg.Login("github.com", "test-user", "test-token", "ssh", false)

	// Then it returns success and the user is set
	require.NoError(t, err)

	user, err := authCfg.User("github.com")
	require.NoError(t, err)
	require.Equal(t, "test-user", user)
}

func TestLoginSetsGitProtocolForProvidedHost(t *testing.T) {
	// Given we are logged in
	authCfg := newTestAuthConfig(t)
	_, err := authCfg.Login("github.com", "test-user", "test-token", "ssh", false)
	require.NoError(t, err)

	// When we get the git protocol
	gitProtocol := authCfg.GitProtocol("github.com")

	// Then it returns the git protocol we provided on login
	require.Equal(t, "ssh", gitProtocol)
}

func TestLoginAddsHostIfNotAlreadyAdded(t *testing.T) {
	// Given we are logged in
	authCfg := newTestAuthConfig(t)
	_, err := authCfg.Login("github.com", "test-user", "test-token", "ssh", false)
	require.NoError(t, err)

	// When we get the hosts
	hosts := authCfg.Hosts()

	// Then it includes our logged in host
	require.Contains(t, hosts, "github.com")
}

func TestLogoutRemovesHostAndKeyringToken(t *testing.T) {
	// Given we are logged into a host
	keyring.MockInit()
	authCfg := newTestAuthConfig(t)
	_, err := authCfg.Login("github.com", "test-user", "test-token", "ssh", true)
	require.NoError(t, err)

	// When we logout
	err = authCfg.Logout("github.com")

	// Then we return success, and the host and token are removed from the config and keyring
	require.NoError(t, err)

	requireNoKey(t, authCfg.cfg, []string{hosts, "github.com"})
	_, err = keyring.Get(keyringServiceName("github.com"), "")
	require.ErrorIs(t, err, keyring.ErrNotFound)
}

// Note that I'm not sure this test enforces particularly desirable behaviour
// since it leads users to believe a token has been removed when really
// that might have failed for some reason.
//
// The original intention here is that if the logout fails, the user can't
// really do anything to recover. On the other hand, a user might
// want to rectify this manually, for example if there were on a shared machine.
func TestLogoutIgnoresErrorsFromConfigAndKeyring(t *testing.T) {
	// Given we have keyring that errors, and a config that
	// doesn't even have a hosts key (which would cause Remove to fail)
	keyring.MockInitWithError(errors.New("test-explosion"))
	authCfg := newTestAuthConfig(t)

	// When we logout
	err := authCfg.Logout("github.com")

	// Then it returns success anyway, suppressing the errors
	require.NoError(t, err)
}

func requireKeyWithValue(t *testing.T, cfg *ghConfig.Config, keys []string, value string) {
	t.Helper()

	actual, err := cfg.Get(keys)
	require.NoError(t, err)

	require.Equal(t, value, actual)
}

func requireNoKey(t *testing.T, cfg *ghConfig.Config, keys []string) {
	t.Helper()

	_, err := cfg.Get(keys)
	var keyNotFoundError *ghConfig.KeyNotFoundError
	require.ErrorAs(t, err, &keyNotFoundError)
}

// Post migration tests

func TestUserWorksRightAfterMigration(t *testing.T) {
	// Given we have logged in before migration
	authCfg := newTestAuthConfig(t)
	_, err := preMigrationLogin(authCfg, "github.com", "test-user", "test-token", "ssh", false)
	require.NoError(t, err)

	// When we migrate
	var m migration.MultiAccount
	require.NoError(t, ghConfig.Migrate(authCfg.cfg, m))

	// Then we can still get the user correctly
	user, err := authCfg.User("github.com")
	require.NoError(t, err)
	require.Equal(t, "test-user", user)
}

func TestGitProtocolWorksRightAfterMigration(t *testing.T) {
	// Given we have logged in before migration with a non-default git protocol
	authCfg := newTestAuthConfig(t)
	_, err := preMigrationLogin(authCfg, "github.com", "test-user", "test-token", "ssh", false)
	require.NoError(t, err)

	// When we migrate
	var m migration.MultiAccount
	require.NoError(t, ghConfig.Migrate(authCfg.cfg, m))

	// Then we can still get the git protocol correctly
	gitProtocol := authCfg.GitProtocol("github.com")
	require.Equal(t, "ssh", gitProtocol)
}

func TestHostsWorksRightAfterMigration(t *testing.T) {
	// Given we have logged in before migration
	authCfg := newTestAuthConfig(t)
	_, err := preMigrationLogin(authCfg, "ghe.io", "test-user", "test-token", "ssh", false)
	require.NoError(t, err)

	// When we migrate
	var m migration.MultiAccount
	require.NoError(t, ghConfig.Migrate(authCfg.cfg, m))

	// Then we can still get the hosts correctly
	hosts := authCfg.Hosts()
	require.Contains(t, hosts, "ghe.io")
}

func TestDefaultHostWorksRightAfterMigration(t *testing.T) {
	// Given we have logged in before migration to an enterprise host
	authCfg := newTestAuthConfig(t)
	_, err := preMigrationLogin(authCfg, "ghe.io", "test-user", "test-token", "ssh", false)
	require.NoError(t, err)

	// When we migrate
	var m migration.MultiAccount
	require.NoError(t, ghConfig.Migrate(authCfg.cfg, m))

	// Then the default host is still the enterprise host
	defaultHost, source := authCfg.DefaultHost()
	require.Equal(t, "ghe.io", defaultHost)
	require.Equal(t, "hosts", source)
}

func TestTokenWorksRightAfterMigration(t *testing.T) {
	// Given we have logged in before migration
	authCfg := newTestAuthConfig(t)
	_, err := preMigrationLogin(authCfg, "github.com", "test-user", "test-token", "ssh", false)
	require.NoError(t, err)

	// When we migrate
	var m migration.MultiAccount
	require.NoError(t, ghConfig.Migrate(authCfg.cfg, m))

	// Then we can still get the token correctly
	token, source := authCfg.Token("github.com")
	require.Equal(t, "test-token", token)
	require.Equal(t, "oauth_token", source)
}

func TestLogoutRigthAfterMigrationRemovesHost(t *testing.T) {
	// Given we have logged in before migration
	authCfg := newTestAuthConfig(t)
	_, err := preMigrationLogin(authCfg, "github.com", "test-user", "test-token", "ssh", false)
	require.NoError(t, err)

	// When we migrate and logout
	var m migration.MultiAccount
	require.NoError(t, ghConfig.Migrate(authCfg.cfg, m))

	require.NoError(t, authCfg.Logout("github.com"))

	// Then the host is removed from the config
	requireNoKey(t, authCfg.cfg, []string{hosts, "github.com"})
}

func TestLoginInsecurePostMigrationUsesConfigForToken(t *testing.T) {
	// Given we have not logged in
	authCfg := newTestAuthConfig(t)

	// When we migrate and login with insecure storage
	var m migration.MultiAccount
	require.NoError(t, ghConfig.Migrate(authCfg.cfg, m))

	insecureStorageUsed, err := authCfg.Login("github.com", "test-user", "test-token", "", false)

	// Then it returns success, notes that insecure storage was used, and stores the token in the config
	// both under the host and under the user
	require.NoError(t, err)

	require.True(t, insecureStorageUsed, "expected to use insecure storage")
	requireKeyWithValue(t, authCfg.cfg, []string{hosts, "github.com", oauthToken}, "test-token")
	requireKeyWithValue(t, authCfg.cfg, []string{hosts, "github.com", "users", "test-user", oauthToken}, "test-token")
}

func TestLoginPostMigrationSetsGitProtocol(t *testing.T) {
	// Given we have logged in after migration
	authCfg := newTestAuthConfig(t)

	var m migration.MultiAccount
	require.NoError(t, ghConfig.Migrate(authCfg.cfg, m))

	_, err := authCfg.Login("github.com", "test-user", "test-token", "ssh", false)
	require.NoError(t, err)

	// When we get the git protocol
	gitProtocol := authCfg.GitProtocol("github.com")

	// Then it returns the git protocol we provided on login
	require.Equal(t, "ssh", gitProtocol)
}

func TestLoginPostMigrationSetsUser(t *testing.T) {
	// Given we have logged in after migration
	authCfg := newTestAuthConfig(t)

	var m migration.MultiAccount
	require.NoError(t, ghConfig.Migrate(authCfg.cfg, m))

	_, err := authCfg.Login("github.com", "test-user", "test-token", "ssh", false)
	require.NoError(t, err)

	// When we get the user
	user, err := authCfg.User("github.com")

	// Then it returns success and the user we provided on login
	require.NoError(t, err)
	require.Equal(t, "test-user", user)
}

func TestLoginSecurePostMigrationRemovesTokenFromConfig(t *testing.T) {
	// Given we have logged in insecurely
	authCfg := newTestAuthConfig(t)
	_, err := preMigrationLogin(authCfg, "github.com", "test-user", "test-token", "", false)
	require.NoError(t, err)

	// When we migrate and login again with secure storage
	var m migration.MultiAccount
	require.NoError(t, ghConfig.Migrate(authCfg.cfg, m))

	keyring.MockInit()
	_, err = authCfg.Login("github.com", "test-user", "test-token", "", true)

	// Then it returns success, having removed the old insecure oauth token entry
	require.NoError(t, err)
	fmt.Println(authCfg.cfg)
	requireNoKey(t, authCfg.cfg, []string{hosts, "github.com", oauthToken})
	requireNoKey(t, authCfg.cfg, []string{hosts, "github.com", "users", "test-user", oauthToken})
}

// Copied and pasted directly from the trunk branch before doing any work on
// login, plus the addition of AuthConfig as the first arg since it is a method
// receiver in the real implementation.
func preMigrationLogin(c *AuthConfig, hostname, username, token, gitProtocol string, secureStorage bool) (bool, error) {
	var setErr error
	if secureStorage {
		if setErr = keyring.Set(keyringServiceName(hostname), "", token); setErr == nil {
			// Clean up the previous oauth_token from the config file.
			_ = c.cfg.Remove([]string{hosts, hostname, oauthToken})
		}
	}
	insecureStorageUsed := false
	if !secureStorage || setErr != nil {
		c.cfg.Set([]string{hosts, hostname, oauthToken}, token)
		insecureStorageUsed = true
	}

	c.cfg.Set([]string{hosts, hostname, "user"}, username)

	if gitProtocol != "" {
		c.cfg.Set([]string{hosts, hostname, "git_protocol"}, gitProtocol)
	}
	return insecureStorageUsed, ghConfig.Write(c.cfg)
}
