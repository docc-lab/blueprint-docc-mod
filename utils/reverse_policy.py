"""Tomislav-RetCtx: configure SDK reverse routing through collector discovery."""

import math

POLICIES = ("ttl", "probability", "inverse_depth", "depth_linear")


def validate(policy=None, probability=None):
    if policy is not None and policy not in POLICIES:
        raise ValueError("--reverse-policy must be " + "|".join(POLICIES))
    if policy == "probability":
        if (type(probability) not in (float, int) or not 0 <= probability <= 1
                or not math.isfinite(probability)):
            raise ValueError("--reverse-policy probability requires --reverse-probability in [0,1]")
    elif probability is not None:
        raise ValueError("--reverse-probability requires --reverse-policy probability")


def configure(config_map, policy=None, probability=None):
    validate(policy, probability)
    if policy is not None:
        config_map["reverse_policy"] = policy
        config_map.pop("reverse_probability", None)
        if policy == "probability":
            config_map["reverse_probability"] = probability
    else:
        # Preserve custom YAML when no override was supplied, validating it
        # with the same policy/p pairing as the collector and SDK.
        existing = config_map.get("reverse_policy", "ttl")
        if existing not in POLICIES:
            raise ValueError("invalid config_map.reverse_policy")
        if existing != "probability" and "reverse_probability" in config_map:
            raise ValueError("config_map.reverse_probability requires probability policy")
        value = config_map.get("reverse_probability")
        if isinstance(value, str):
            try:
                value = float(value)
            except ValueError:
                raise ValueError("invalid config_map.reverse_probability") from None
        validate(existing, value)
