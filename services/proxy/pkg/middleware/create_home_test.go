package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	userv1beta1 "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpcpb "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/proxy/pkg/router"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	cs3mocks "github.com/opencloud-eu/reva/v2/tests/cs3mocks/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
)

// newCreateHomeHandler wires the middleware around a no-op handler with a mocked gateway, and
// returns both so a test can count the CreateHome calls the middleware actually made.
func newCreateHomeHandler(t *testing.T, status rpcpb.Code) (http.Handler, *cs3mocks.GatewayAPIClient) {
	t.Helper()

	gatewayClient := cs3mocks.NewGatewayAPIClient(t)
	gatewayClient.On("CreateHome", mock.Anything, mock.Anything, mock.Anything).
		Return(&provider.CreateHomeResponse{Status: &rpcpb.Status{Code: status}}, nil).Maybe()

	selectorName := "CreateHomeGatewaySelector"
	gatewaySelector := pool.GetSelector[gateway.GatewayAPIClient](
		selectorName,
		"eu.opencloud.api.gateway",
		func(cc grpc.ClientConnInterface) gateway.GatewayAPIClient { return gatewayClient },
	)
	t.Cleanup(func() { pool.RemoveSelector(selectorName + "eu.opencloud.api.gateway") })

	handler := CreateHome(
		Logger(log.NewLogger()),
		WithRevaGatewaySelector(gatewaySelector),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	return handler, gatewayClient
}

// requestFor builds a request that passes shouldServe: a token header on a protected route,
// carrying the given user in the context.
func requestFor(userID string, userType userv1beta1.UserType) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/graph/v1.0/me", nil)
	req.Header.Set(revactx.TokenHeader, "some-token")
	u := &userv1beta1.User{Id: &userv1beta1.UserId{OpaqueId: userID, Type: userType}}
	// The middleware reads the role IDs out of the user's opaque and bails out with a 500 if they
	// are absent, so a user fixture without them never reaches the gateway at all.
	u.Opaque = utils.AppendJSONToOpaque(u.Opaque, "roles", []string{"d7beeea8-8ff4-406b-8fb6-ab2dd81e6b11"})
	ctx := revactx.ContextSetUser(req.Context(), u)
	ctx = router.SetRoutingInfo(ctx, router.RoutingInfo{})
	return req.WithContext(ctx)
}

// The point of the change: the outcome of CreateHome is stable per user, so asking the gateway on
// every request is work whose answer is already known. Before the cache this made one gRPC call per
// request; for a user whose role cannot create a space it also made one error log line per request.
func TestCreateHomeAsksTheGatewayOncePerUser(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status rpcpb.Code
	}{
		// The user has a home already — the overwhelmingly common case, and silent today because
		// ALREADY_EXISTS is filtered from the log. Silent, but still a call per request.
		{"home already exists", rpcpb.Code_CODE_ALREADY_EXISTS},
		// The user's role has no CreateSpaces permission, so the answer can never become OK.
		// This is the case that also logs an error every single time.
		{"role may not create a home", rpcpb.Code_CODE_PERMISSION_DENIED},
		{"home created", rpcpb.Code_CODE_OK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, gatewayClient := newCreateHomeHandler(t, tc.status)

			for range 5 {
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, requestFor("user-"+tc.name, userv1beta1.UserType_USER_TYPE_PRIMARY))
				assert.Equal(t, http.StatusOK, rec.Code, "the middleware must not affect the response")
			}

			gatewayClient.AssertNumberOfCalls(t, "CreateHome", 1)
		})
	}
}

// Two users must not share one another's cache entry — a naive implementation keyed on anything
// but the user would make the first user's answer stand in for everyone's.
func TestCreateHomeCachesPerUser(t *testing.T) {
	handler, gatewayClient := newCreateHomeHandler(t, rpcpb.Code_CODE_ALREADY_EXISTS)

	for _, id := range []string{"alice", "bob", "alice", "bob", "carol"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, requestFor(id, userv1beta1.UserType_USER_TYPE_PRIMARY))
	}

	gatewayClient.AssertNumberOfCalls(t, "CreateHome", 3)
}

// The pre-existing skip for lightweight and service users must keep working: those never reach the
// gateway at all, cache or no cache.
func TestCreateHomeSkipsLightweightAndServiceUsers(t *testing.T) {
	for _, ut := range []userv1beta1.UserType{
		userv1beta1.UserType_USER_TYPE_LIGHTWEIGHT,
		userv1beta1.UserType_USER_TYPE_SERVICE,
	} {
		t.Run(ut.String(), func(t *testing.T) {
			handler, gatewayClient := newCreateHomeHandler(t, rpcpb.Code_CODE_OK)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, requestFor("lw-"+ut.String(), ut))

			assert.Equal(t, http.StatusOK, rec.Code)
			gatewayClient.AssertNumberOfCalls(t, "CreateHome", 0)
		})
	}
}
