package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
)

func TestNewPluginRegistry_HasHelmfile(t *testing.T) {
	registry := NewPluginRegistry()
	p, ok := registry[config.ToolHelmfile]
	require.True(t, ok)
	assert.Equal(t, "helmfile", p.Name())
}

func TestNew_ConstructsOrchestratorWithDefaults(t *testing.T) {
	client := newTestRedisClient(t)
	appAuth, err := github.NewAppAuth(1, []byte(testPEMKey))
	require.NoError(t, err)

	o := New(appAuth, (lock.LockManager)(nil), nil, NewPluginRegistry(), client, true, "turnip-server:9090", "ghcr.io/ivanvc/turnip-runner:test")
	require.NotNil(t, o)
	assert.Equal(t, defaultStartTimeout, o.startTimeout)
	assert.Equal(t, defaultSweepInterval, o.sweepInterval)
	assert.True(t, o.minimizeOutdatedPlanComments)
	assert.Equal(t, "turnip-server:9090", o.runnerServerAddr)
	require.NotNil(t, o.installationClient)
}

// testPEMKey is a throwaway RSA private key, generated solely for this
// test (openssl genrsa -traditional 2048) and used by nothing else —
// valid enough for ghinstallation.NewAppsTransport to parse; its content
// never makes a real request in this test.
const testPEMKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEoQIBAAKCAQEAwLCdxfVzkVnXjFvc3Fnf+69sTo1coO5o+2RvTFqNWpnrfin3
MEh3HI/HLKYOoraKowVCqCG4peSZJFBZzLr0xkoFaeCaCe2blfX/TbJ+T2yVxg5m
m+Yms9TB3xGjnECC1zZ1yUS18tuQmLzPeQ6oO3AlsjXtgwCdd4nY0Z9Lz/epzB2d
/NXC5hYzzjnr4S9IytDtCwHtOmmWA+zrMEQMOmyu3aPLxwp5z+p3K0QvqHFs4Pwq
70c0lg0erOSXpbcC0KyyW/hOLU1H/Jm2KehHD4dUy5/DLqKhAegxmNME/6oZ20wh
EHD9pp+dDSoC/J3xnJcAeu4twpkUCE7UiKeJ8QIDAQABAoH/I2BJlw55KTZVXG+B
HPNjK9IJEGLjnqgmngDSbjIxwXCAy6jouPSU6al757aU+UqKKWPN2RBX1h0iAIi/
89ZfGgh89fNGVIxLBD0loh4jEnHdDX+XVwfqExn8ffe/EfDGFLzq4wi6XtvwsBn5
/T+zozXztcQw1txGDKxCIjocsRF07Bf/ZWLFDDdCGXvTSKz9h0B2VDqMfQFm9mnx
Q7WaqWAK3p7aHOWg7VwTkMjHNEg4GcNgYDrkfzTZCBn22I7GxE0RZehbBgplQItC
1J/YsBqVGYClcX1R7KY8ujR0Kbqw+Haa24wTgdQEZ53fqLxYPcIYfk73uk+tyrMW
JN+RAoGBAPt+4fZuaUCINO5UnOl5Su/QO34jOgtp6W3g1VOFKw2C2AwOxPqMfhq+
ib4gyBCyDoaFYY7eRdAC6CBGwS+808sDvITrWXraYCx0uQ+65XgvZrhiLRueyukB
aJUA0VNg3eaeYiw3JANq8/ufPpFavQ0U66kax470fG47hzMLh551AoGBAMQkG2j5
RXqO+tbSKwkeWq8EKIxsMAl7x4/M8ft2hqWMHmsBujS5MS8BGeUFy/NzVbNAd1SA
FbfDoGSXb68BR1FEWJEfDRqByiTUdl2VwzSp7h4G8VC1OwzlbmQS4AbD7ayz5tJr
iUC+6iLiS/hM22zTXUA30mgjvdZCjGD6nsYNAoGAJ15WWvAvs7Veq8w8/+NH0sCC
/5zeEjlTHCss2bUn5yaLUV/if+IMP32CLGwPRWXKFk681dN/lC9QTPUmeqWRdd8w
3JHG4Q9cLLlc2tSD5UtuRgDAVOmHk+/vghutqOKp+dbjQU6kaZCNft8PuUs9+tVC
iqcxg/RLoinZYSk14p0CgYEAtSvA4cK2KZGeIwV4WPDfxJ9rsOFRscDSwYIF1kdR
8eIuSpsK2x5gTtGOkJw9Gf9jnrIiRzwUU2xwX9n9gEIHFQqzYEC1QtG13TUerCzk
ZWW9G6FAD1OHWs8lm2xP4A/kHs0BnUVVPyfZbyVmFNExMSE/Fk05nZW+CQXpUr7M
H5UCgYA+BJoDPmL3i/UGsE9hbru3jfhVeSQSI3BkT6n9fGQ8A311kPBHNG4Q0OCa
aZ+a58iExkaXwUuJLO2oQ0n0WgyiRG9gOEl/m898Dk0f2X9YRwTXuMWQ0E26fuai
zHNQOwP6Jdq1w3swCK7HOXCS2HYXpWtbEmEgxqwC58FbxhCFhQ==
-----END RSA PRIVATE KEY-----`
