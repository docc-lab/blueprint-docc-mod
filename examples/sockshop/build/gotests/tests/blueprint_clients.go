
package tests

import (
	"context"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/order"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/catalogue"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/payment"
	"blueprint/testclients/clients"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/frontend"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/user"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/cart"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/shipping"
)

// Auto-generated code by the Blueprint gotests plugin.
func init() {
	// Initialize the clientlib early so that it can pick up command-line flags
	clientlib := clients.NewClientLibrary("tests")

	
	ordersRegistry.Register("order_service", func(ctx context.Context) (order.OrderService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client order.OrderService
		err = namespace.Get("order_service.client", &client)
		return client, err
	})
	
	catalogueRegistry.Register("catalogue_service", func(ctx context.Context) (catalogue.CatalogueService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client catalogue.CatalogueService
		err = namespace.Get("catalogue_service.client", &client)
		return client, err
	})
	
	frontendRegistry.Register("frontend", func(ctx context.Context) (frontend.Frontend, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client frontend.Frontend
		err = namespace.Get("frontend.client", &client)
		return client, err
	})
	
	ordersRegistry.Register("frontend", func(ctx context.Context) (order.OrderService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client order.OrderService
		err = namespace.Get("frontend.client", &client)
		return client, err
	})
	
	userServiceRegistry.Register("user_service", func(ctx context.Context) (user.UserService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client user.UserService
		err = namespace.Get("user_service.client", &client)
		return client, err
	})
	
	paymentServiceRegistry.Register("payment_service", func(ctx context.Context) (payment.PaymentService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client payment.PaymentService
		err = namespace.Get("payment_service.client", &client)
		return client, err
	})
	
	cartRegistry.Register("cart_service", func(ctx context.Context) (cart.CartService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client cart.CartService
		err = namespace.Get("cart_service.client", &client)
		return client, err
	})
	
	shippingRegistry.Register("shipping_service", func(ctx context.Context) (shipping.ShippingService, error) {
		// Build the client library
		namespace, err := clientlib.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Get and return the client
		var client shipping.ShippingService
		err = namespace.Get("shipping_service.client", &client)
		return client, err
	})
	
}
