package graph

import (
	communityv1 "github.com/yourmate/backend/gen/community/v1"
	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/services/gateway/storage"
)

// Resolver is the root resolver. It holds the gRPC clients the gateway fans
// out to; every resolver is a thin translation between GraphQL and gRPC.
type Resolver struct {
	Identity     identityv1.IdentityServiceClient
	Market       marketplacev1.MarketplaceServiceClient
	CommunitySvc communityv1.CommunityServiceClient
	Store        *storage.Store
	Cookies      CookieConfig
	Limiter      *Limiter
}
