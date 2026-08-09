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
	"github.com/opencloud-eu/opencloud/services/proxy/pkg/userroles"
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
func requestFor(userType userv1beta1.UserType, decorate func(*userv1beta1.User)) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/graph/v1.0/me", nil)
	req.Header.Set(revactx.TokenHeader, "some-token")
	u := &userv1beta1.User{Id: &userv1beta1.UserId{OpaqueId: "user-id", Type: userType}}
	// The middleware reads the role ids out of the user's opaque and bails out with a 500 if they
	// are absent, so a user fixture without them never reaches the gateway at all.
	u.Opaque = utils.AppendJSONToOpaque(u.Opaque, "roles", []string{"d7beeea8-8ff4-406b-8fb6-ab2dd81e6b11"})
	if decorate != nil {
		decorate(u)
	}
	ctx := revactx.ContextSetUser(req.Context(), u)
	ctx = router.SetRoutingInfo(ctx, router.RoutingInfo{})
	return req.WithContext(ctx)
}

func withCreateSpaces(allowed bool) func(*userv1beta1.User) {
	return func(u *userv1beta1.User) {
		u.Opaque = utils.AppendJSONToOpaque(u.Opaque, userroles.CreateSpacesOpaqueKey, allowed)
	}
}

// The point of the change: for a user whose role carries no CreateSpaces permission the gateway can
// only ever answer PERMISSION_DENIED, and the middleware logs that denial as an error. Asking at all
// is the defect; the role assigner has already recorded the answer.
func TestCreateHomeSkipsTheGatewayWhenTheRoleMayNotCreateSpaces(t *testing.T) {
	handler, gatewayClient := newCreateHomeHandler(t, rpcpb.Code_CODE_PERMISSION_DENIED)

	for range 5 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, requestFor(userv1beta1.UserType_USER_TYPE_PRIMARY, withCreateSpaces(false)))
		assert.Equal(t, http.StatusOK, rec.Code, "the middleware must not affect the response")
	}

	gatewayClient.AssertNumberOfCalls(t, "CreateHome", 0)
}

// A user who may have a personal space must still get one, so the call has to survive the change.
func TestCreateHomeStillAsksWhenTheRoleMayCreateSpaces(t *testing.T) {
	handler, gatewayClient := newCreateHomeHandler(t, rpcpb.Code_CODE_OK)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestFor(userv1beta1.UserType_USER_TYPE_PRIMARY, withCreateSpaces(true)))

	assert.Equal(t, http.StatusOK, rec.Code)
	gatewayClient.AssertNumberOfCalls(t, "CreateHome", 1)
}

// An assigner that could not resolve the role bundles leaves the key unset. Reading that as "may
// not" would deny a home to a user entitled to one, so the middleware behaves as it did before.
func TestCreateHomeAsksWhenThePermissionIsUnknown(t *testing.T) {
	handler, gatewayClient := newCreateHomeHandler(t, rpcpb.Code_CODE_OK)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestFor(userv1beta1.UserType_USER_TYPE_PRIMARY, nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	gatewayClient.AssertNumberOfCalls(t, "CreateHome", 1)
}

// The pre-existing skip for lightweight and service users must keep working.
func TestCreateHomeSkipsLightweightAndServiceUsers(t *testing.T) {
	for _, ut := range []userv1beta1.UserType{
		userv1beta1.UserType_USER_TYPE_LIGHTWEIGHT,
		userv1beta1.UserType_USER_TYPE_SERVICE,
	} {
		t.Run(ut.String(), func(t *testing.T) {
			handler, gatewayClient := newCreateHomeHandler(t, rpcpb.Code_CODE_OK)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, requestFor(ut, withCreateSpaces(true)))

			assert.Equal(t, http.StatusOK, rec.Code)
			gatewayClient.AssertNumberOfCalls(t, "CreateHome", 0)
		})
	}
}
