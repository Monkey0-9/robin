import json

SCHEMA_PATH = "config_schema.json"
CPP_PATH = "config.h"
RUST_PATH = "../risk-analytics/src/config.rs"
GO_PATH = "../gateway/config.go"

def to_camel_case(s):
    if s.startswith("ROBIN_"):
        s = s[6:]
    parts = s.split("_")
    out = []
    for p in parts:
        if p == "SHM":
            out.append("SHM")
        elif p == "MSG":
            out.append("Msg")
        elif p == "PORT":
            out.append("Port")
        elif p == "MCAST":
            out.append("Mcast")
        elif p == "BPS":
            out.append("BPS")
        elif p == "DEV":
            out.append("Dev")
        else:
            out.append(p.capitalize())
    return "".join(out)

def write_cpp(schema):
    lines = [
        "// ============================================================================",
        "// Robin Trading Platform — Shared IPC Configuration (AUTO-GENERATED)",
        "// ============================================================================",
        "#pragma once",
        ""
    ]
    
    for section, items in schema.items():
        lines.append(f"// {section}")
        for k, v in items.items():
            if isinstance(v, dict):
                val = v["value"]
                t = v["type"]
                if t == "u64" and isinstance(val, int):
                    val = f"{val}ULL"
                elif t in ["usize", "u32"] and isinstance(val, int):
                    val = f"{val}u"
                elif t == "i64" and isinstance(val, int):
                    val = f"{val}LL"
                elif isinstance(val, str) and val.startswith("0x"):
                    if t == "u64":
                        val = f"{val}ULL"
                    else:
                        val = f"{val}u"
                lines.append(f"#define {k.ljust(27)} {val}")
            else:
                lines.append(f'#define {k.ljust(27)} "{v}"')
        lines.append("")

    with open(CPP_PATH, "w", encoding="utf-8") as f:
        f.write("\n".join(lines))

def write_rust(schema):
    lines = [
        "// ============================================================================",
        "// Robin Trading Platform — Shared Configuration (AUTO-GENERATED)",
        "// ============================================================================",
        ""
    ]
    
    for section, items in schema.items():
        lines.append(f"/// {section}")
        for k, v in items.items():
            rust_k = k.replace("ROBIN_", "")
            if isinstance(v, dict):
                val = v["value"]
                t = v["type"]
                if isinstance(val, int):
                    if t == "f64":
                        val = f"{val}.0"
                lines.append(f"pub const {rust_k}: {t} = {val};")
            else:
                lines.append(f'pub const {rust_k}: &str = "{v}";')
        lines.append("")

    with open(RUST_PATH, "w", encoding="utf-8") as f:
        f.write("\n".join(lines))

def write_go(schema):
    lines = [
        "package main",
        "",
        "// ============================================================================",
        "// Robin Trading Platform — Shared Go Configuration (AUTO-GENERATED)",
        "// ============================================================================",
        ""
    ]
    
    for section, items in schema.items():
        lines.append(f"// {section}")
        lines.append("const (")
        for k, v in items.items():
            go_k = to_camel_case(k)
            if isinstance(v, dict):
                val = v["value"]
                t = v["type"]
                if isinstance(val, str) and val.startswith("0x"):
                    pass
                elif t == "f64" and isinstance(val, (int, float)):
                    val = f"{val:.2f}"
                lines.append(f"\t{go_k.ljust(20)} = {val}")
            else:
                lines.append(f'\t{go_k.ljust(20)} = "{v}"')
        lines.append(")")
        lines.append("")

    with open(GO_PATH, "w", encoding="utf-8") as f:
        f.write("\n".join(lines))

if __name__ == "__main__":
    with open(SCHEMA_PATH, "r", encoding="utf-8") as f:
        schema = json.load(f)
    
    write_cpp(schema)
    write_rust(schema)
    write_go(schema)
    print("Successfully generated config.h, config.rs, and config.go")
