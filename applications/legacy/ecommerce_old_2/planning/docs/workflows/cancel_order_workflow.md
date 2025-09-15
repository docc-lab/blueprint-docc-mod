# Workflow: cancel_order

**Endpoint:** `POST /orders/cancel`
**Description:** Cancel order and process refund
**Business Purpose:** Order cancellation and refund processing

## Call Tree

```
└─ [0] Order Service.cancel_order (depth: 1)
   ├─ [0] Authentication Service.validate_token (depth: 2)
   ├─ [1] Order Validation Service.validate_cancellation (depth: 2) (depends on: [0])
   │  └─ [0] Order Status Service.check_order_status (depth: 3)
   │     └─ [0] Order Cache Service.get_cached_order (depth: 4)
   │        └─ [0] Order Processing Service.process_order_data (depth: 5)
   ├─ [2] Inventory Service.restore_inventory (depth: 2) (depends on: [1])
   │  └─ [0] Stock Service.update_stock_levels (depth: 3)
   │     └─ [0] Inventory Management Service.manage_inventory_restoration (depth: 4)
   │        └─ [0] Inventory Cache Service.get_cached_inventory (depth: 5)
   │           └─ [0] Inventory Processing Service.process_inventory_restoration (depth: 6)
   ├─ [3] Payment Service.process_refund (depth: 2) (depends on: [2])
   │  └─ [0] Refund Service.initiate_refund (depth: 3)
   │     └─ [0] Payment Processing Service.process_payment_refund (depth: 4)
   │        └─ [0] Payment Cache Service.get_cached_payment_data (depth: 5)
   │           └─ [0] Payment Validation Service.validate_refund_eligibility (depth: 6)
   ├─ [4] Order Management Service.update_order_status (depth: 2) (depends on: [3])
   │  └─ [0] Order Status Management Service.manage_order_status (depth: 3)
   │     └─ [0] Order Cache Service.update_cached_order (depth: 4)
   │        └─ [0] Order Processing Service.process_status_update (depth: 5)
   ├─ [5] Notification Service.send_cancellation_notification (depth: 2) (depends on: [4])
   │  └─ [0] Email Service.send_cancellation_email (depth: 3)
   │     └─ [0] Email Processing Service.process_cancellation_email (depth: 4)
   │        └─ [0] Email Cache Service.get_cached_email_template (depth: 5)
   │           └─ [0] Email Template Service.generate_cancellation_template (depth: 6)
   ├─ [6] Analytics Service.track_cancellation (depth: 2) (depends on: [5])
   │  └─ [0] Analytics Processing Service.process_cancellation_analytics (depth: 3)
   │     └─ [0] Analytics Cache Service.get_cached_analytics_data (depth: 4)
   │        └─ [0] Analytics Aggregation Service.aggregate_cancellation_data (depth: 5)
   └─ [7] Order History Service.log_cancellation (depth: 2) (depends on: [6])
```
