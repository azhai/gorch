#!/usr/bin/env python3
"""Turn a stack profile into a flame graph SVG, with no third-party deps.

Two input formats are understood:

  --malloc-history FILE   macOS `malloc_history <pid> -callTree -fullStacks`
                          output. This is how gorch's heap is profiled: Odin's
                          heap allocator forwards to libc malloc, so the
                          Allocations stack log sees every `make`/`new`.
                          Node widths are bytes, so the graph answers "who
                          holds the memory", not "who burns CPU".
  --collapsed FILE        "a;b;c 1234" lines, the format produced by
                          stackcollapse.pl, `sample` conversions, and
                          --write-collapsed below.

Usage:
    tools/flamegraph.py --malloc-history /tmp/calltree.txt -o /tmp/heap.svg
    tools/flamegraph.py --malloc-history calltree.txt --write-collapsed out.txt
"""

import argparse
import re
import sys
from html import escape

# "      10 (16.3M) thread_start  (in libsystem_pthread.dylib) + 8  [0x18bf80c1c]"
TREE_RE = re.compile(r"^(?P<prefix>\s*(?:[+!:]\s+|\s{2})*)(?P<count>\d+)\s+\((?P<size>[^)]+)\)\s+(?P<rest>.*)$")
SIZE_RE = re.compile(r"^([\d.]+)\s*([KMG]?)B?(?:ytes)?$")


def parse_size(text):
    m = SIZE_RE.match(text.strip())
    if not m:
        return 0
    value = float(m.group(1))
    unit = m.group(2)
    scale = {"": 1, "K": 1024, "M": 1024 ** 2, "G": 1024 ** 3}[unit]
    return int(value * scale)


def clean_symbol(rest):
    """Drop ' (in binary) + offset [addr]' and the -123 mangling suffix."""
    name = rest.split("  (in ")[0].strip()
    name = re.sub(r"^\[unknown\]$", "[unknown]", name)
    name = re.sub(r"-\d+$", "", name)
    return name or "[unknown]"


class Node:
    __slots__ = ("name", "value", "children", "depth")

    def __init__(self, name, depth=0):
        self.name = name
        self.value = 0
        self.children = []
        self.depth = depth


def parse_malloc_history(path):
    root = Node("all")
    stack = [root]
    with open(path, "r", errors="replace") as fh:
        for line in fh:
            if not line.strip() or line.startswith(("malloc_history", "Process:", "Path:", "Stack")) :
                continue
            m = TREE_RE.match(line.rstrip("\n"))
            if not m:
                continue
            prefix = m.group("prefix")
            if "<< TOTAL >>" in m.group("rest"):
                root.value = parse_size(m.group("size"))
                continue
            depth = max(1, (len(prefix) - 6) // 2 + 1) if len(prefix) >= 6 else 1
            node = Node(clean_symbol(m.group("rest")), depth)
            node.value = parse_size(m.group("size"))
            # Re-anchor to the parent at depth-1; anything deeper than the
            # current stack means the line belongs to the previous node.
            while len(stack) > depth:
                stack.pop()
            while len(stack) < depth:
                stack.append(stack[-1])
            parent = stack[depth - 1]
            parent.children.append(node)
            stack.append(node)
    return root


def parse_collapsed(path):
    root = Node("all")
    with open(path, "r", errors="replace") as fh:
        for line in fh:
            line = line.rstrip("\n")
            if not line or line.startswith("#"):
                continue
            try:
                stack, count = line.rsplit(" ", 1)
                value = int(float(count))
            except ValueError:
                continue
            node = root
            for frame in stack.split(";"):
                found = None
                for child in node.children:
                    if child.name == frame:
                        found = child
                        break
                if found is None:
                    found = Node(frame, node.depth + 1)
                    node.children.append(found)
                node = found
            node.value += value
            root.value += value
    return root


def fold(node):
    """Collapse single-child chains so tall thin towers do not waste rows."""
    node.children.sort(key=lambda c: -c.value)
    return node


def collect_collapsed(node, path, out):
    if node.children:
        for child in node.children:
            collect_collapsed(child, path + [child.name], out)
    if node.value > 0 and not node.children:
        out.append(";".join(path) + " %d" % node.value)


def layout(node, x, y, width, rows, min_px):
    """Assign a rectangle to every visible node. Children take the width they
    account for; the leftover is the node's own allocations."""
    rows.append((x, y, width, node))
    child_total = sum(c.value for c in node.children)
    if child_total <= 0 or width <= min_px:
        return
    scale = width / float(node.value)
    cursor = x
    for child in node.children:
        w = child.value * scale
        if w >= min_px:
            layout(child, cursor, y + 1, w, rows, min_px)
        cursor += w


def color_for(name):
    # Stable, muted palette keyed by frame name; gorch frames get warm tones,
    # system frames stay grey so the eye lands on application code first.
    if "(in " in name:
        return "#7d8590"
    h = 0
    for ch in name:
        h = (h * 131 + ord(ch)) & 0xFFFFFFFF
    palette = ["#c26d4f", "#b8863b", "#7f9a55", "#4f8c8c", "#5b7bb5", "#8a6aa8", "#a85f7c"]
    return palette[h % len(palette)]


def render_svg(root, title, width=1400, row_height=18, font=13):
    rows = []
    layout(root, 0, 0, width, rows, min_px=0.4)
    depth = max(y for _, y, _, _ in rows) + 1
    top = 56
    height = top + depth * row_height + 40
    parts = [
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" '
        f'viewBox="0 0 {width} {height}" font-family="-apple-system,BlinkMacSystemFont,Segoe UI,Helvetica,Arial,sans-serif">',
        '<rect width="100%" height="100%" fill="#16181c"/>',
        f'<text x="16" y="26" fill="#e6e6e6" font-size="16" font-weight="600">{escape(title)}</text>',
        f'<text x="16" y="46" fill="#9aa4af" font-size="12">width = live heap bytes per call stack '
        f'(total {root.value / 1048576:.1f} MiB); hover a frame for the exact size</text>',
    ]
    for x, y, w, node in rows:
        yy = top + y * row_height
        fill = color_for(node.name)
        opacity = 0.55 + 0.45 * min(1.0, node.value / max(1, root.value))
        label = node.name
        max_chars = int(w / (font * 0.56))
        if max_chars >= 4 and len(label) > max_chars:
            label = label[: max_chars - 1] + "…"
        elif max_chars < 4:
            label = ""
        parts.append(
            f'<g><rect x="{x:.1f}" y="{yy}" width="{max(w - 0.6, 0.4):.1f}" height="{row_height - 1}" '
            f'fill="{fill}" fill-opacity="{opacity:.2f}" stroke="#16181c" stroke-width="0.5">'
            f'<title>{escape(node.name)} — {node.value / 1024:.1f} KiB ({node.value} bytes)</title></rect>'
        )
        if label:
            parts.append(
                f'<text x="{x + 4:.1f}" y="{yy + row_height - 5}" fill="#101215" font-size="{font}" '
                f'pointer-events="none">{escape(label)}</text>'
            )
        parts.append("</g>")
    parts.append("</svg>")
    return "\n".join(parts)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    src = ap.add_mutually_exclusive_group(required=True)
    src.add_argument("--malloc-history", metavar="FILE")
    src.add_argument("--collapsed", metavar="FILE")
    ap.add_argument("-o", "--output", default="flamegraph.svg")
    ap.add_argument("--write-collapsed", metavar="FILE", help="also dump folded stacks")
    ap.add_argument("--title", default="gorch heap flame graph")
    ap.add_argument("--width", type=int, default=1400)
    args = ap.parse_args()

    root = parse_malloc_history(args.malloc_history) if args.malloc_history else parse_collapsed(args.collapsed)
    fold(root)
    if root.value <= 0:
        sys.exit("no allocations parsed; was malloc_history run with -callTree?")

    if args.write_collapsed:
        out = []
        collect_collapsed(root, [], out)
        with open(args.write_collapsed, "w") as fh:
            fh.write("\n".join(out) + "\n")

    svg = render_svg(root, args.title, width=args.width)
    with open(args.output, "w") as fh:
        fh.write(svg)
    print(f"{args.output}: {root.value / 1048576:.2f} MiB across {len(root.children)} roots")


if __name__ == "__main__":
    main()
