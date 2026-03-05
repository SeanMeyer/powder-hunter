# CLI Contract: Cost Optimization Flags

## `powder-hunter run` — New Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--budget` | float64 | 0 (disabled) | Monthly budget limit in USD. When set, evaluations are halted once estimated cumulative spend reaches this amount. First evaluations for new storms are always allowed. |

### Behavior

- `--budget 5.00` → halt non-essential evaluations when monthly spend estimate reaches $5.00. Log warning at $4.00 (80%).
- `--budget 0` or omitted → no budget enforcement (default, backward-compatible).
- Budget resets at the start of each calendar month (UTC).

## `powder-hunter run` — Updated Log Output

### Run Summary (JSON structured log)

**Before** (existing):
```json
{"level":"INFO","msg":"pipeline complete","scanned":12,"evaluated":8,"posted":3,"expired":1}
```

**After** (extended):
```json
{"level":"INFO","msg":"pipeline complete","scanned":12,"evaluated":4,"posted":2,"expired":1,"skipped_unchanged":3,"skipped_cooldown":2,"skipped_budget":0}
```

### Skip Log Entry (new)

Emitted for each skipped evaluation:
```json
{"level":"INFO","msg":"evaluation skipped","region_id":"co_i70_corridor","storm_id":42,"skip_reason":"unchanged_weather","hours_since_last_eval":11.5}
```

### Budget Warning (new)

Emitted when estimated monthly spend reaches 80% of budget:
```json
{"level":"WARN","msg":"monthly budget approaching limit","budget_usd":5.00,"spent_usd":4.12,"remaining_usd":0.88,"calls_this_month":274}
```

## Backward Compatibility

- All new flags have defaults that preserve existing behavior.
- The run summary log line adds new fields but does not remove or rename existing fields.
- Existing `--dry-run`, `--region`, `--loop`, `--interval`, `--verbose`, and `--db` flags are unchanged.
- `trace` and `replay` commands are unaffected — they do not use the pipeline's Evaluate stage and are exempt from all cost optimizations.
