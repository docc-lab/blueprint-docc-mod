from setuptools import setup, find_packages

setup(
    name="k8s_convert",
    version="0.1.0",
    packages=find_packages(),
    install_requires=[
        "pyyaml>=6.0",
    ],
    entry_points={
        'console_scripts': [
            'k8s-convert=k8s_convert.main:main',
        ],
    },
    python_requires=">=3.7",
) 