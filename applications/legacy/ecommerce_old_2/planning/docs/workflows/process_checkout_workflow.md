# Workflow: process_checkout

**Endpoint:** `POST /checkout/process`
**Description:** Process complete checkout with payment
**Business Purpose:** Complete order processing

## Call Tree

```
└─ [0] Checkout Service.process_checkout (depth: 1)
   ├─ [0] Authentication Service.validate_token (depth: 2)
   ├─ [1] Authorization Service.check_checkout_permission (depth: 2) (depends on: [0])
   ├─ [2] Checkout Validation Service.validate_checkout_data (depth: 2) (depends on: [1])
   │  ├─ [0] User Validation Service.validate_user_data (depth: 3)
   │  ├─ [1] Address Service.validate_shipping_address (depth: 3)
   │  └─ [2] Address Service.validate_billing_address (depth: 3)
   ├─ [3] Shopping Cart Service.get_cart_items (depth: 2) (depends on: [2])
   ├─ [4] Inventory Service.check_availability (depth: 2) (depends on: [3])
   │  ├─ [0] Stock Service.verify_stock_levels (depth: 3)
   │  └─ [1] Inventory Validation Service.validate_reservations (depth: 3) (depends on: [0])
   ├─ [5] Pricing Service.calculate_total (depth: 2) (depends on: [3, 4])
   │  ├─ [0] Price Calculation Service.calculate_base_price (depth: 3)
   │  ├─ [1] Tax Service.calculate_tax (depth: 3) (depends on: [0])
   │  │  ├─ [0] Tax Validation Service.validate_tax_rules (depth: 4)
   │  │  └─ [1] Tax Calculation Service.compute_tax_amount (depth: 4) (depends on: [0])
   │  ├─ [2] Shipping Service.calculate_shipping (depth: 3) (depends on: [0])
   │  │  ├─ [0] Shipping Validation Service.validate_shipping_options (depth: 4)
   │  │  └─ [1] Shipping Processing Service.compute_shipping_cost (depth: 4) (depends on: [0])
   │  └─ [3] Discount Service.apply_discounts (depth: 3) (depends on: [0])
   │     ├─ [0] Coupon Service.validate_coupons (depth: 4)
   │     └─ [1] Discount Service.apply_discount_rules (depth: 4) (depends on: [0])
   ├─ [6] Payment Service.process_payment (depth: 2) (depends on: [5])
   │  ├─ [0] Payment Validation Service.validate_payment_method (depth: 3)
   │  ├─ [1] Payment Processing Service.authorize_payment (depth: 3) (depends on: [0])
   │  │  ├─ [0] Payment Gateway Service.process_authorization (depth: 4)
   │  │  └─ [1] Payment History Service.log_authorization (depth: 4) (depends on: [0])
   │  └─ [2] Payment Processing Service.capture_payment (depth: 3) (depends on: [1])
   │     └─ [0] Payment Gateway Service.process_capture (depth: 4)
   ├─ [7] Order Service.create_order (depth: 2) (depends on: [4, 5, 6])
   │  ├─ [0] Order Validation Service.validate_order_data (depth: 3)
   │  ├─ [1] Order Processing Service.create_order_record (depth: 3) (depends on: [0])
   │  └─ [2] Order Tracking Service.initialize_tracking (depth: 3) (depends on: [1])
   ├─ [8] Inventory Service.reserve_inventory (depth: 2) (depends on: [7])
   │  ├─ [0] Stock Service.reserve_stock (depth: 3)
   │  └─ [1] Inventory Alert Service.check_low_stock (depth: 3) (depends on: [0])
   ├─ [9] Shopping Cart Service.clear_cart (depth: 2) (depends on: [7])
   ├─ [10] Order History Service.log_order_creation (depth: 2) (depends on: [7, 8])
   └─ [11] Email Service.send_order_confirmation (depth: 2) (depends on: [7])
      ├─ [0] Content Management Service.generate_order_email_template (depth: 3)
      └─ [1] Email Service.send_confirmation_email (depth: 3) (depends on: [0])
```
