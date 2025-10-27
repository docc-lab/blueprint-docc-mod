#!/usr/bin/env python3

import os
import sys
import logging
import argparse
from typing import Optional
from .convert import K8sConvert
from .push_images import push_images

def setup_logging(level: str = "INFO"):
    """Set up logging configuration."""
    logging.basicConfig(
        level=getattr(logging, level.upper()),
        format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
    )

def parse_args():
    """Parse command line arguments."""
    parser = argparse.ArgumentParser(description='Convert Docker Compose to Kubernetes manifests')
    
    # Required arguments
    parser.add_argument('compose_dir', help='Directory containing docker-compose.yml')
    parser.add_argument('--output-dir', '-o', required=True, help='Output directory for Kubernetes manifests')
    
    # Optional arguments
    parser.add_argument('--namespace', '-n', default='default', help='Kubernetes namespace (default: default)')
    parser.add_argument('--registry', '-r', help='Docker registry URL for pushing images')
    parser.add_argument('--tag', '-t', default='latest', help='Tag for Docker images (default: latest)')
    parser.add_argument('--use-kompose', '-k', action='store_true', help='Use kompose for conversion')
    parser.add_argument('--log-level', '-l', default='INFO', choices=['DEBUG', 'INFO', 'WARNING', 'ERROR'],
                      help='Logging level (default: INFO)')
    parser.add_argument('--push-images', '-p', action='store_true', help='Push images to registry after building')
    
    return parser.parse_args()

def main():
    """Main entry point."""
    args = parse_args()
    setup_logging(args.log_level)
    logger = logging.getLogger(__name__)

    try:
        # Create output directory if it doesn't exist
        os.makedirs(args.output_dir, exist_ok=True)

        # Initialize converter
        converter = K8sConvert(
            compose_dir=args.compose_dir,
            output_dir=args.output_dir,
            namespace=args.namespace,
            registry_url=args.registry,
            use_kompose=args.use_kompose
        )

        # Convert docker-compose to Kubernetes manifests
        logger.info("Starting conversion...")
        converter.convert()
        logger.info(f"Conversion complete. Manifests written to {args.output_dir}")

        # Push images if requested
        if args.push_images and args.registry:
            logger.info("Pushing images to registry...")
            push_images(
                compose_dir=args.compose_dir,
                registry=args.registry,
                tag=args.tag
            )
            logger.info("Images pushed successfully")
        elif args.push_images and not args.registry:
            logger.error("Cannot push images: registry URL not provided")
            sys.exit(1)

    except Exception as e:
        logger.error(f"Error during conversion: {e}")
        sys.exit(1)

if __name__ == '__main__':
    main() 