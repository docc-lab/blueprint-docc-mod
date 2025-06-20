import yaml
import sys
import os

main_services = [
    "user_service",
    "payment_service",
    "cart_service",
    "shipping_service",
    "order_service",
    "catalogue_service",
    "frontend",
]

for service in main_services:
    # Convert service names with underscores to hyphens for K8s compatibility
    k8s_service_name = service.replace('_', '-')
    
    main_deployment_file = f"{k8s_service_name}-deployment.yaml"
    sidecar_service_name = f"{k8s_service_name}-greeter-sidecar"
    sidecar_deployment_file = f"{sidecar_service_name}-deployment.yaml"
    sidecar_service_file = f"{sidecar_service_name}-service.yaml"

    if os.path.exists(main_deployment_file) and os.path.exists(sidecar_deployment_file):
        print(f"  Merging {sidecar_service_name} into {k8s_service_name}...")
        
        with open(main_deployment_file, 'r') as f:
            main_deployment = yaml.safe_load(f)
        
        with open(sidecar_deployment_file, 'r') as f:
            sidecar_deployment = yaml.safe_load(f)
            
        # Get the sidecar container
        sidecar_container = sidecar_deployment['spec']['template']['spec']['containers'][0]
        
        # Add to main deployment
        main_deployment['spec']['template']['spec']['containers'].append(sidecar_container)
        
        # Write back to main deployment file
        with open(main_deployment_file, 'w') as f:
            yaml.dump(main_deployment, f)
            
        # Clean up sidecar files
        os.remove(sidecar_deployment_file)
        if os.path.exists(sidecar_service_file):
            os.remove(sidecar_service_file)

print("Sidecar merging complete.") 