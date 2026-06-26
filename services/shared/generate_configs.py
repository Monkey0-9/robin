import json
import os

SCHEMA_PATH = "config_schema.json"
CPP_PATH = "config.h"
RUST_PATH = "../risk-analytics/src/config.rs"

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

    # For rust we need to handle some formatting or special cases, but the above is generally fine
    with open(RUST_PATH, "w", encoding="utf-8") as f:
        f.write("\n".join(lines))

if __name__ == "__main__":
    with open(SCHEMA_PATH, "r", encoding="utf-8") as f:
        schema = json.load(f)
    
    write_cpp(schema)
    write_rust(schema)
    print("Successfully generated config.h and config.rs")
