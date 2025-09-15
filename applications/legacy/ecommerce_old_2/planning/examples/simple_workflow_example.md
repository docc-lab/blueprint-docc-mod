# Workflow: get_product_details

**Endpoint:** `GET /products/{productId}`
**Description:** Get detailed product information
**Business Purpose:** Display product details to customer

## Call Tree

```
├─ [0] Product Catalog Service.get_product (depth: 1)
├─ [1] Product Cache Service.check_cache (depth: 1)
└─ [2] Product Validation Service.validate_product (depth: 1) (depends on: [0])
   ├─ [0] Inventory Service.check_inventory (depth: 2)
   └─ [1] Pricing Service.get_pricing (depth: 2) (depends on: [0])
      └─ [0] Price Cache Service.check_price_cache (depth: 3)
```
