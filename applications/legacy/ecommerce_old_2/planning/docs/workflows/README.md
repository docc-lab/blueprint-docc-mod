# Workflow Documentation

This directory contains tree visualizations of workflow JSON files.

## Available Workflows

- **[process_checkout](process_checkout_workflow.md)** - `POST /checkout/process`
  - Process complete checkout with payment

- **[initiate_checkout](initiate_checkout_workflow.md)** - `POST /checkout/initiate`
  - Start checkout process with validation

- **[validate_checkout](validate_checkout_workflow.md)** - `POST /checkout/validate`
  - Validate checkout data and availability

- **[product_search](product_search_workflow.md)** - `POST /products/search`
  - Product search with advanced filtering

- **[cancel_order](cancel_order_workflow.md)** - `POST /orders/cancel`
  - Cancel order and process refund

## Usage

To regenerate these documents, run:
```bash
python generate_docs.py [input_dir] [output_dir]
```

## Workflow Format

These workflows follow the JSON format defined in `workflow_format.md`.
Each workflow shows the service call hierarchy and dependencies.