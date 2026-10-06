package jobs

import "errors"

// A completed business check may deliberately skip an obsolete external effect.
// Preserve its reason while distinguishing it from a failed or accepted effect.
var ErrBatchItemSkipped = errors.New("任务目标已跳过")
