package hotelnw

// Tomislav-RetCtx: zero-work leaf services (no downstream calls in the real app either). Same
// interfaces and constructor signatures as hotelreservation; the database / cache handles are
// accepted so the wiring (and so the deployment topology) is unchanged, and never used.

import (
	"context"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
)

// UserService implements the user service from hotel reservation.
type UserService interface {
	// Returns true if the user with the provided credentials exists.
	CheckUser(ctx context.Context, username string, password string) (bool, error)
}

type UserServiceImpl struct{ userDB backend.NoSQLDatabase }

func NewUserServiceImpl(ctx context.Context, userDB backend.NoSQLDatabase) (UserService, error) {
	return &UserServiceImpl{userDB: userDB}, nil
}

func (u *UserServiceImpl) CheckUser(ctx context.Context, username string, password string) (bool, error) {
	return true, nil
}

// RecommendationService implements the recommendation service from hotel reservation.
type RecommendationService interface {
	// Returns the recommended hotel IDs for the location and ranking criterion.
	GetRecommendations(ctx context.Context, require string, lat float64, lon float64) ([]string, error)
}

type RecommendationServiceImpl struct{ recommendDB backend.NoSQLDatabase }

func NewRecommendationServiceImpl(ctx context.Context, recommendDB backend.NoSQLDatabase) (RecommendationService, error) {
	return &RecommendationServiceImpl{recommendDB: recommendDB}, nil
}

func (r *RecommendationServiceImpl) GetRecommendations(ctx context.Context, require string, lat float64, lon float64) ([]string, error) {
	return []string{}, nil
}

// ReservationService implements the reservation service from hotel reservation.
type ReservationService interface {
	// Makes a reservation; returns the hotels reserved.
	MakeReservation(ctx context.Context, customerName string, hotelIds []string, inDate string, outDate string, roomNumber int64) ([]string, error)
	// Returns the subset of hotels with availability.
	CheckAvailability(ctx context.Context, customerName string, hotelIDs []string, inDate string, outDate string, roomNumber int64) ([]string, error)
}

type ReservationServiceImpl struct {
	reserveCache backend.Cache
	reserveDB    backend.NoSQLDatabase
}

func NewReservationServiceImpl(ctx context.Context, reserveCache backend.Cache, reserveDB backend.NoSQLDatabase) (ReservationService, error) {
	return &ReservationServiceImpl{reserveCache: reserveCache, reserveDB: reserveDB}, nil
}

func (r *ReservationServiceImpl) MakeReservation(ctx context.Context, customerName string, hotelIds []string, inDate string, outDate string, roomNumber int64) ([]string, error) {
	return hotelIds, nil
}

func (r *ReservationServiceImpl) CheckAvailability(ctx context.Context, customerName string, hotelIDs []string, inDate string, outDate string, roomNumber int64) ([]string, error) {
	return hotelIDs, nil
}

// GeoService implements the geo service from hotel reservation.
type GeoService interface {
	// Returns the IDs of the hotels near the location.
	Nearby(ctx context.Context, lat float64, lon float64) ([]string, error)
}

type GeoServiceImpl struct{ geoDB backend.NoSQLDatabase }

func NewGeoServiceImpl(ctx context.Context, geoDB backend.NoSQLDatabase) (GeoService, error) {
	return &GeoServiceImpl{geoDB: geoDB}, nil
}

func (g *GeoServiceImpl) Nearby(ctx context.Context, lat float64, lon float64) ([]string, error) {
	return []string{}, nil
}

// RateService implements the rate service from hotel reservation.
type RateService interface {
	// Returns the rate plans of the hotels for the date range.
	GetRates(ctx context.Context, hotelIDs []string, inDate string, outDate string) ([]RatePlan, error)
}

type RateServiceImpl struct {
	rateCache backend.Cache
	rateDB    backend.NoSQLDatabase
}

func NewRateServiceImpl(ctx context.Context, rateCache backend.Cache, rateDB backend.NoSQLDatabase) (RateService, error) {
	return &RateServiceImpl{rateCache: rateCache, rateDB: rateDB}, nil
}

func (r *RateServiceImpl) GetRates(ctx context.Context, hotelIDs []string, inDate string, outDate string) ([]RatePlan, error) {
	return []RatePlan{}, nil
}

// ProfileService implements the profile service from hotel reservation.
type ProfileService interface {
	// Returns the profiles of the hotels.
	GetProfiles(ctx context.Context, hotelIds []string, locale string) ([]HotelProfile, error)
}

type ProfileServiceImpl struct {
	profileCache backend.Cache
	profileDB    backend.NoSQLDatabase
}

func NewProfileServiceImpl(ctx context.Context, profileCache backend.Cache, profileDB backend.NoSQLDatabase) (ProfileService, error) {
	return &ProfileServiceImpl{profileCache: profileCache, profileDB: profileDB}, nil
}

func (p *ProfileServiceImpl) GetProfiles(ctx context.Context, hotelIds []string, locale string) ([]HotelProfile, error) {
	return []HotelProfile{}, nil
}
