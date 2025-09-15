# Workflow Documentation

This directory contains tree visualizations of workflow JSON files.

## Available Workflows

- **[complex_checkout_process](complex_workflow_example.md)** - `POST /checkout/process`
  - Complex checkout process with payment and shipping

- **[get_product_details](simple_workflow_example.md)** - `GET /products/{productId}`
  - Get detailed product information

## Usage

To regenerate these documents, run:
```bash
python generate_docs.py [input_dir] [output_dir]
```

## Workflow Format

These workflows follow the JSON format defined in `workflow_format.md`.
Each workflow shows the service call hierarchy and dependencies.