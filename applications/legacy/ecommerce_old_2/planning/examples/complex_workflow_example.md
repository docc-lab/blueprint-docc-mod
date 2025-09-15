# Workflow: complex_checkout_process

**Endpoint:** `POST /checkout/process`
**Description:** Complex checkout process with payment and shipping
**Business Purpose:** Process customer checkout with full validation

## Call Tree

```
├─ [0] Checkout Service.initiate_checkout (depth: 1)
├─ [1] Checkout Validation Service.validate_checkout (depth: 1) (depends on: [0])
│  ├─ [0] Shopping Cart Service.get_cart (depth: 2)
│  │  ├─ [0] Cart Validation Service.validate_cart (depth: 3)
│  │  └─ [1] Cart Processing Service.process_cart (depth: 3) (depends on: [0])
│  ├─ [1] Product Catalog Service.get_products (depth: 2) (depends on: [0])
│  │  ├─ [0] Product Cache Service.check_cache (depth: 3)
│  │  └─ [1] Product Validation Service.validate_products (depth: 3) (depends on: [0])
│  └─ [2] Inventory Service.check_inventory (depth: 2) (depends on: [1])
│     ├─ [0] Stock Service.check_stock (depth: 3)
│     └─ [1] Inventory Validation Service.validate_inventory (depth: 3) (depends on: [0])
├─ [2] Checkout Processing Service.process_checkout (depth: 1) (depends on: [1])
│  ├─ [0] Pricing Service.calculate_prices (depth: 2)
│  │  ├─ [0] Price Cache Service.check_price_cache (depth: 3)
│  │  └─ [1] Price Calculation Service.calculate_final_prices (depth: 3) (depends on: [0])
│  ├─ [1] Discount Service.apply_discounts (depth: 2) (depends on: [0])
│  │  └─ [0] Coupon Service.validate_coupons (depth: 3)
│  ├─ [2] Tax Service.calculate_tax (depth: 2) (depends on: [0])
│  │  ├─ [0] Tax Validation Service.validate_tax_rules (depth: 3)
│  │  └─ [1] Tax Calculation Service.calculate_final_tax (depth: 3) (depends on: [0])
│  └─ [3] Shipping Service.calculate_shipping (depth: 2) (depends on: [0])
│     ├─ [0] Shipping Validation Service.validate_shipping (depth: 3)
│     └─ [1] Shipping Processing Service.process_shipping (depth: 3) (depends on: [0])
├─ [3] Payment Service.process_payment (depth: 1) (depends on: [2])
│  ├─ [0] Payment Validation Service.validate_payment (depth: 2)
│  └─ [1] Payment Processing Service.process_transaction (depth: 2) (depends on: [0])
│     └─ [0] Payment History Service.log_payment (depth: 3)
└─ [4] Order Service.create_order (depth: 1) (depends on: [3])
   ├─ [0] Order Validation Service.validate_order (depth: 2)
   └─ [1] Order Processing Service.process_order (depth: 2) (depends on: [0])
      ├─ [0] Order Tracking Service.create_tracking (depth: 3)
      └─ [1] Order History Service.log_order (depth: 3) (depends on: [0])
```
