#!/usr/bin/env python3

import os
import yaml
import subprocess
import logging
from typing import Dict, Any, Optional
from pathlib import Path

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

class ImagePusher:
    def __init__(
        self,
        compose_dir: str,
        registry_url: str,
        username: Optional[str] = None,
        password: Optional[str] = None,
        insecure: bool = False
    ):
        self.compose_dir = Path(compose_dir)
        self.registry_url = registry_url.rstrip('/')
        self.username = username
        self.password = password
        self.insecure = insecure
        self.compose_file = self.compose_dir / 'docker-compose.yml'
        self.services = {}
        self.load_compose()

    def load_compose(self):
        """Load Docker Compose configuration."""
        with open(self.compose_file, 'r') as f:
            self.compose = yaml.safe_load(f)
        self.services = self.compose.get('services', {})

    def build_and_push_images(self):
        """Build and push all service images."""
        try:
            # Login to registry if credentials provided
            if self.username and self.password:
                self._login_to_registry()

            # Process each service
            for svc_name, svc in self.services.items():
                if 'build' in svc:
                    self._build_and_push_service(svc_name, svc)
                elif 'image' in svc:
                    self._push_existing_image(svc_name, svc)

        except Exception as e:
            logger.error(f"Failed to build and push images: {str(e)}")
            raise
        finally:
            # Logout from registry if we logged in
            if self.username and self.password:
                self._logout_from_registry()

    def _login_to_registry(self):
        """Login to Docker registry."""
        try:
            cmd = ['docker', 'login', self.registry_url]
            if self.insecure:
                cmd.append('--insecure')
            if self.username:
                cmd.extend(['-u', self.username])
            if self.password:
                cmd.extend(['-p', self.password])
            
            subprocess.run(cmd, check=True, capture_output=True)
            logger.info(f"Successfully logged in to {self.registry_url}")
        except subprocess.CalledProcessError as e:
            logger.error(f"Failed to login to registry: {e.stderr.decode()}")
            raise

    def _logout_from_registry(self):
        """Logout from Docker registry."""
        try:
            subprocess.run(
                ['docker', 'logout', self.registry_url],
                check=True,
                capture_output=True
            )
            logger.info(f"Successfully logged out from {self.registry_url}")
        except subprocess.CalledProcessError as e:
            logger.warning(f"Failed to logout from registry: {e.stderr.decode()}")

    def _build_and_push_service(self, svc_name: str, svc: Dict[str, Any]):
        """Build and push a service image."""
        try:
            # Get build context and Dockerfile
            build_config = svc.get('build', {})
            if isinstance(build_config, str):
                context = build_config
                dockerfile = 'Dockerfile'
            else:
                context = build_config.get('context', '.')
                dockerfile = build_config.get('dockerfile', 'Dockerfile')

            # Resolve context and dockerfile paths relative to the compose file
            context_path = (self.compose_dir / context).resolve()
            dockerfile_path = (context_path / dockerfile.lstrip('./')).resolve()

            if not dockerfile_path.exists():
                raise FileNotFoundError(f"Dockerfile not found: {dockerfile_path}")

            # Get image name
            image_name = svc.get('image', svc_name)
            if not image_name:
                image_name = svc_name

            # Build image
            logger.info(f"Building image for service {svc_name}...")
            build_cmd = [
                'docker', 'build',
                '-t', image_name,
                '-f', str(dockerfile_path),
                str(context_path)
            ]
            subprocess.run(build_cmd, check=True, capture_output=True)

            # Push image
            self._push_image(image_name)

        except subprocess.CalledProcessError as e:
            logger.error(f"Failed to build/push service {svc_name}: {e.stderr.decode()}")
            raise
        except Exception as e:
            logger.error(f"Failed to build/push service {svc_name}: {str(e)}")
            raise

    def _push_existing_image(self, svc_name: str, svc: Dict[str, Any]):
        """Push an existing image to registry."""
        try:
            image_name = svc['image']
            logger.info(f"Pushing existing image {image_name} for service {svc_name}...")
            # Pull the image if it doesn't exist locally
            try:
                subprocess.run(['docker', 'inspect', image_name], check=True, capture_output=True)
            except subprocess.CalledProcessError:
                logger.info(f"Image {image_name} not found locally. Pulling...")
                subprocess.run(['docker', 'pull', image_name], check=True, capture_output=True)
            self._push_image(image_name)
        except subprocess.CalledProcessError as e:
            logger.error(f"Failed to push image for service {svc_name}: {e.stderr.decode()}")
            raise
        except Exception as e:
            logger.error(f"Failed to push image for service {svc_name}: {str(e)}")
            raise

    def _push_image(self, image_name: str):
        """Push a single image to registry."""
        try:
            # Tag image for registry
            tagged_image = f'{self.registry_url}/{image_name}'
            tag_cmd = ['docker', 'tag', image_name, tagged_image]
            subprocess.run(tag_cmd, check=True, capture_output=True)

            # Push image using Docker's existing configuration
            push_cmd = ['docker', 'push', tagged_image]
            subprocess.run(push_cmd, check=True, capture_output=True)

            logger.info(f"Successfully pushed image {tagged_image}")

        except subprocess.CalledProcessError as e:
            logger.error(f"Failed to push image {image_name}: {e.stderr.decode()}")
            raise

def push_images(compose_dir: str, registry: str, tag: str = 'latest', username: Optional[str] = None, password: Optional[str] = None, insecure: bool = False):
    """Push images to registry."""
    pusher = ImagePusher(
        compose_dir=compose_dir,
        registry_url=registry,
        username=username,
        password=password,
        insecure=insecure
    )
    pusher.build_and_push_images()

def main():
    import argparse
    parser = argparse.ArgumentParser(
        description='Build and push Docker images for Blueprint services'
    )
    parser.add_argument(
        'compose_dir',
        help='Path to Blueprint Docker Compose directory'
    )
    parser.add_argument(
        'registry_url',
        help='Docker registry URL'
    )
    parser.add_argument(
        '--username',
        help='Registry username'
    )
    parser.add_argument(
        '--password',
        help='Registry password'
    )
    parser.add_argument(
        '--insecure',
        action='store_true',
        help='Use insecure registry connection'
    )

    args = parser.parse_args()

    try:
        push_images(
            args.compose_dir,
            args.registry_url,
            args.username,
            args.password,
            args.insecure
        )
    except Exception as e:
        logger.error(f"Failed to process images: {str(e)}")
        sys.exit(1)

if __name__ == '__main__':
    main() 