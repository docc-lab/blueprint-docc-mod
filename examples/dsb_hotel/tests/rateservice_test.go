package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gitlab.mpi-sws.org/cld/blueprint/examples/dsb_hotel/workflow/hotelreservation"
	"gitlab.mpi-sws.org/cld/blueprint/runtime/core/registry"
	"gitlab.mpi-sws.org/cld/blueprint/runtime/plugins/simplecache"
	"gitlab.mpi-sws.org/cld/blueprint/runtime/plugins/simplenosqldb"
)

var rateServiceRegistry = registry.NewServiceRegistry[hotelreservation.RateService]("rate_service")

func init() {
	rateServiceRegistry.Register("local", func(ctx context.Context) (hotelreservation.RateService, error) {
		db, err := simplenosqldb.NewSimpleNoSQLDB(ctx)
		if err != nil {
			return nil, err
		}
		cache, err := simplecache.NewSimpleCache(ctx)
		if err != nil {
			return nil, err
		}
		return hotelreservation.NewRateServiceImpl(ctx, cache, db)
	})
}

func TestGetRates(t *testing.T) {
	ctx := context.Background()
	service, err := rateServiceRegistry.Get(ctx)
	assert.NoError(t, err)

	plans, err := service.GetRates(ctx, []string{"1", "2", "3", "12", "9"}, "2015-04-09", "2015-04-10")
	assert.NoError(t, err)
	assert.Len(t, plans, 5)
}