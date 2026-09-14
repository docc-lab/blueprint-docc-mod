package tests

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/blueprint-uservices/blueprint/examples/dsb_hotel/workflow/hotelreservation"
	"github.com/blueprint-uservices/blueprint/runtime/core/registry"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/simplecache"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/simplenosqldb"
	"github.com/stretchr/testify/assert"
)

func TestConcurrentCheckAvailability(t *testing.T) {
	service, err := reservationServiceRegistry.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Warm the local test cache before concurrent reads. Its in-memory map is
	// deliberately simple; deployed services use the memcached client.
	if _, err := service.CheckAvailability(context.Background(), "warmup", []string{"1"}, "2015-04-09", "2015-04-10", 1); err != nil {
		t.Fatal(err)
	}
	const requests = 16
	var workers sync.WaitGroup
	errors := make(chan error, requests)
	for i := 0; i < requests; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			hotels, err := service.CheckAvailability(context.Background(), "concurrent", []string{"1"}, "2015-04-09", "2015-04-10", 1)
			if err == nil && (len(hotels) != 1 || hotels[0] != "1") {
				err = fmt.Errorf("unexpected available hotels: %v", hotels)
			}
			errors <- err
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
}

var reservationServiceRegistry = registry.NewServiceRegistry[hotelreservation.ReservationService]("reservation_service")

func init() {
	reservationServiceRegistry.Register("local", func(ctx context.Context) (hotelreservation.ReservationService, error) {
		db, err := simplenosqldb.NewSimpleNoSQLDB(ctx)
		if err != nil {
			return nil, err
		}
		cache, err := simplecache.NewSimpleCache(ctx)
		if err != nil {
			return nil, err
		}

		return hotelreservation.NewReservationServiceImpl(ctx, cache, db)
	})
}

func TestCheckAvailability(t *testing.T) {
	ctx := context.Background()
	service, err := reservationServiceRegistry.Get(ctx)
	assert.NoError(t, err)

	hotels, err := service.CheckAvailability(ctx, "Vaastav", []string{"1"}, "2015-04-09", "2015-04-10", 1)
	assert.NoError(t, err)
	assert.Len(t, hotels, 1)
	assert.Equal(t, hotels[0], "1")
}

func TestMakeReservation(t *testing.T) {
	ctx := context.Background()
	service, err := reservationServiceRegistry.Get(ctx)
	assert.NoError(t, err)

	hotels, err := service.MakeReservation(ctx, "Vaastav", []string{"1"}, "2015-04-09", "2015-04-10", 1)
	assert.NoError(t, err)
	assert.Len(t, hotels, 1)
	assert.Equal(t, hotels[0], "1")
}
