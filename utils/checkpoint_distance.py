"""Tomislav-RetCtx: validate and switch fixed/ranged collector CPD options."""


def validate(cpd=None, minimum=None, maximum=None):
    if minimum is not None or maximum is not None:
        if cpd is not None:
            raise ValueError("use --cpd OR --cpd-min/--cpd-max")
        if (type(minimum) is not int or type(maximum) is not int
                or not 1 <= minimum <= maximum <= 256):
            raise ValueError("require integer 1 <= --cpd-min <= --cpd-max <= 256")
    elif cpd is not None and (type(cpd) is not int or cpd < 1):
        raise ValueError("--cpd must be a positive integer")


def configure(config_map, cpd=None, minimum=None, maximum=None, default=None):
    validate(cpd, minimum, maximum)
    if minimum is not None:
        config_map.pop("cpd", None)
        config_map.update(cpd_min=minimum, cpd_max=maximum)
    elif cpd is not None:
        config_map.pop("cpd_min", None)
        config_map.pop("cpd_max", None)
        config_map["cpd"] = cpd
    elif "cpd_min" in config_map or "cpd_max" in config_map:
        low, high = config_map.get("cpd_min"), config_map.get("cpd_max")
        if low is None or high is None:
            raise ValueError("config_map requires both cpd_min and cpd_max")
        validate(None, low, high)
    elif default is not None:
        config_map.setdefault("cpd", default)
