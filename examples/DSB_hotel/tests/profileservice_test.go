package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gitlab.mpi-sws.org/cld/blueprint/examples/DSB_hotel/workflow/hotelreservation"
	"gitlab.mpi-sws.org/cld/blueprint/runtime/core/registry"
	"gitlab.mpi-sws.org/cld/blueprint/runtime/plugins/simplecache"
	"gitlab.mpi-sws.org/cld/blueprint/runtime/plugins/simplenosqldb"
)

var profileServiceRegistry = registry.NewServiceRegistry[hotelreservation.ProfileService]("profile_service")

func init() {
	profileServiceRegistry.Register("local", func(ctx context.Context) (hotelreservation.ProfileService, error) {
		db, err := simplenosqldb.NewSimpleNoSQLDB(ctx)
		if err != nil {
			return nil, err
		}
		cache, err := simplecache.NewSimpleCache(ctx)
		if err != nil {
			return nil, err
		}

		return hotelreservation.NewProfileServiceImpl(ctx, cache, db)
	})
}

func TestGetProfiles(t *testing.T) {
	ctx := context.Background()
	service, err := profileServiceRegistry.Get(ctx)
	assert.NoError(t, err)

	profiles, err := service.GetProfiles(ctx, []string{"1", "2", "3", "4", "5"}, "en")
	assert.NoError(t, err)
	// Check number of hotels
	assert.Len(t, profiles, 5)

	// Check names of hotels
	assert.Equal(t, "Clift Hotel", profiles[0].Name)
	assert.Equal(t, "W San Francisco", profiles[1].Name)
	assert.Equal(t, "Hotel Zetta", profiles[2].Name)
	assert.Equal(t, "Hotel Vitale", profiles[3].Name)
	assert.Equal(t, "Phoenix Hotel", profiles[4].Name)
}
