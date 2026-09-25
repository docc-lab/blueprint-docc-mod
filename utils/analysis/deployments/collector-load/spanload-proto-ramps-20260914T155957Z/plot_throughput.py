#!/usr/bin/env python3
"""Tomislav-RetCtx: compact separate and paired throughput figures from protobuf ramps."""

import hashlib
import json
from pathlib import Path
import re
import xml.etree.ElementTree as ET

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from PIL import Image


ROOT = Path(__file__).resolve().parent
WIDTH, HEIGHT, DPI = 2.2, 2.1, 300
STYLES = {
    "none": dict(label="Vanilla", color="#333333", marker="o", markersize=3.0,
                 markerfacecolor="#333333", linestyle="-"),
    "pb": dict(label="PB", color="#0072B2", marker="^", markersize=4.5,
               markerfacecolor="white", linestyle="-"),
    "cgpb": dict(label="CGPB", color="#D55E00", marker="s", markersize=3.3,
                 markerfacecolor="white", linestyle="--"),
    "sb": dict(label="SB", color="#009E73", marker="o", markersize=2.1,
               markerfacecolor="white", linestyle=":"),
}
PROFILES = {
    "zero": dict(name="no-semconv", title="No semconv", xmax=1700, ymax=1080,
                 xticks=[0, 400, 800, 1200, 1600], yticks=[0, 250, 500, 750, 1000]),
    "semconv10-example": dict(name="10-semconv", title="10 semconv", xmax=212, ymax=125,
                              xticks=[0, 50, 100, 150, 200], yticks=[0, 40, 80, 120]),
}


def verify_dimensions(stem, width=WIDTH, height=HEIGHT):
    # Tomislav-RetCtx: do not crop with bbox_inches='tight'; keep the requested page width.
    box, = re.findall(rb"/MediaBox\s*\[\s*0\s+0\s+([0-9.]+)\s+([0-9.]+)\s*\]",
                      stem.with_suffix(".pdf").read_bytes())
    assert abs(float(box[0]) / 72 - width) < 1e-6
    assert abs(float(box[1]) / 72 - height) < 1e-6
    svg = ET.parse(stem.with_suffix(".svg")).getroot()
    assert abs(float(svg.attrib["width"].removesuffix("pt")) / 72 - width) < 1e-6
    assert abs(float(svg.attrib["height"].removesuffix("pt")) / 72 - height) < 1e-6
    with Image.open(stem.with_suffix(".png")) as raster:
        assert raster.size == (round(width * DPI), round(height * DPI))
        assert all(abs(value - DPI) < .01 for value in raster.info["dpi"])


def plot_combined(metadata):
    # Tomislav-RetCtx: one shared y-axis label; each panel retains its own scale.
    width = WIDTH * 2
    fig, axes = plt.subplots(1, 2, figsize=(width, HEIGHT), dpi=DPI)
    fig.subplots_adjust(left=.125, right=.985, bottom=.23, top=.84, wspace=.22)
    ylabel = fig.supylabel("Throughput (k spans/s)", x=.015, y=.535, fontsize=9)
    text_artists = [ylabel]
    for ax, (profile, settings) in zip(axes, PROFILES.items()):
        rows = json.loads((ROOT / profile / "results.json").read_text())
        for variant, style in STYLES.items():
            series = sorted((row for row in rows if row["variant"] == variant),
                            key=lambda row: row["target_spans_per_second"])
            ax.plot([row["target_spans_per_second"] / 1000 for row in series],
                    [row["exported_spans_per_second"] / 1000 for row in series],
                    linewidth=1.05, markeredgewidth=.8, **style)
        ax.set(xlim=(0, settings["xmax"]), ylim=(0, settings["ymax"]),
               xticks=settings["xticks"], yticks=settings["yticks"])
        ax.set_xlabel("Offered rate (k spans/s)", labelpad=3)
        ax.set_title(settings["title"], pad=6)
        ax.grid(alpha=.20, linewidth=.5)
        ax.set_axisbelow(True)
        legend = ax.legend(loc="lower right", ncol=2, handlelength=1.05,
                           handletextpad=.3, columnspacing=.6, borderpad=.35,
                           labelspacing=.3, borderaxespad=.3, framealpha=.95,
                           edgecolor="#cccccc", fancybox=False)
        legend.get_frame().set_linewidth(.5)
        text_artists += [ax.title, ax.xaxis.label, *ax.get_xticklabels(),
                         *ax.get_yticklabels(), *legend.get_texts()]
    fig.canvas.draw()
    renderer = fig.canvas.get_renderer()
    assert ylabel.get_window_extent(renderer).x1 + 3 <= min(
        tick.get_window_extent(renderer).x0 for tick in axes[0].get_yticklabels())
    for artist in text_artists:
        bounds = artist.get_window_extent(renderer)
        assert bounds.x0 >= 0 and bounds.y0 >= 0, artist.get_text()
        assert bounds.x1 <= fig.bbox.width and bounds.y1 <= fig.bbox.height, artist.get_text()
    assert all(not ax.get_ylabel() for ax in axes)
    stem = ROOT / "throughput-combined"
    for extension in ("pdf", "svg", "png"):
        fig.savefig(stem.with_suffix("." + extension), dpi=DPI, facecolor="white", bbox_inches=None)
        metadata["figures"].append(stem.with_suffix("." + extension).name)
    plt.close(fig)
    verify_dimensions(stem, width, HEIGHT)
    metadata["combined_figure"] = {"width_inches": width, "height_inches": HEIGHT,
                                   "shared_y_label": True, "independent_y_scales": True}
    print(f"{stem.name}: {width} x {HEIGHT} inches; PDF, SVG, PNG verified")


def main():
    fonts = {"font.size": 9, "axes.titlesize": 10, "axes.labelsize": 9,
             "xtick.labelsize": 8, "ytick.labelsize": 8, "legend.fontsize": 8}
    metadata = {"annotation": "Tomislav-RetCtx: compact throughput-only versions of the corrected protobuf ramps.",
                "width_inches": WIDTH, "height_inches": HEIGHT, "png_dpi": DPI,
                "fonts_points": fonts, "x_field": "target_spans_per_second",
                "x_meaning": "Aggregate offered target, divided across four independent generators",
                "y_field": "exported_spans_per_second", "display_units": "thousand spans/s",
                "collector": "One collector, 1 CPU, 4 GiB, batch 512, protobuf file export to /dev/null",
                "inputs": {}, "figures": []}
    with plt.rc_context({**fonts, "font.family": "DejaVu Sans", "axes.linewidth": .65,
                         "xtick.major.width": .65, "ytick.major.width": .65,
                         "xtick.major.size": 2.5, "ytick.major.size": 2.5,
                         "xtick.major.pad": 2, "ytick.major.pad": 2,
                         "pdf.fonttype": 42, "ps.fonttype": 42, "svg.fonttype": "none",
                         "savefig.bbox": None}):
        for profile, settings in PROFILES.items():
            source = ROOT / profile / "results.json"
            rows = json.loads(source.read_text())
            assert all(row["profile"] == profile and row["export_format"] == "proto" for row in rows)
            metadata["inputs"][profile] = {"path": str(source.relative_to(ROOT)),
                                           "sha256": hashlib.sha256(source.read_bytes()).hexdigest()}
            fig, ax = plt.subplots(figsize=(WIDTH, HEIGHT), dpi=DPI)
            fig.subplots_adjust(left=.25, right=.975, bottom=.23, top=.84)
            for variant, style in STYLES.items():
                series = sorted((row for row in rows if row["variant"] == variant),
                                key=lambda row: row["target_spans_per_second"])
                ax.plot([row["target_spans_per_second"] / 1000 for row in series],
                        [row["exported_spans_per_second"] / 1000 for row in series],
                        linewidth=1.05, markeredgewidth=.8, **style)
            ax.set(xlim=(0, settings["xmax"]), ylim=(0, settings["ymax"]),
                   xticks=settings["xticks"], yticks=settings["yticks"])
            ax.set_xlabel("Offered rate (k spans/s)", labelpad=3)
            ax.set_ylabel("Throughput (k spans/s)", labelpad=3)
            ax.set_title(settings["title"], pad=6)
            ax.grid(alpha=.20, linewidth=.5)
            ax.set_axisbelow(True)
            legend = ax.legend(loc="lower right", ncol=2, handlelength=1.05,
                               handletextpad=.3, columnspacing=.6, borderpad=.35,
                               labelspacing=.3, borderaxespad=.3, framealpha=.95,
                               edgecolor="#cccccc", fancybox=False)
            legend.get_frame().set_linewidth(.5)
            fig.canvas.draw()
            renderer = fig.canvas.get_renderer()
            text_artists = [ax.title, ax.xaxis.label, ax.yaxis.label,
                            *ax.get_xticklabels(), *ax.get_yticklabels(), *legend.get_texts()]
            for artist in text_artists:
                bounds = artist.get_window_extent(renderer)
                assert bounds.x0 >= 0 and bounds.y0 >= 0
                assert bounds.x1 <= fig.bbox.width and bounds.y1 <= fig.bbox.height, artist.get_text()
            stem = ROOT / ("throughput-" + settings["name"])
            for extension in ("pdf", "svg", "png"):
                fig.savefig(stem.with_suffix("." + extension), dpi=DPI, facecolor="white", bbox_inches=None)
                metadata["figures"].append(stem.with_suffix("." + extension).name)
            plt.close(fig)
            verify_dimensions(stem)
            print(f"{stem.name}: {WIDTH} x {HEIGHT} inches; PDF, SVG, PNG verified")
        plot_combined(metadata)
    (ROOT / "throughput-figure-settings.json").write_text(json.dumps(metadata, indent=2) + "\n")


if __name__ == "__main__":
    main()
