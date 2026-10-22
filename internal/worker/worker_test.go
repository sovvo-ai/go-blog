package worker

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeArticleID(t *testing.T) {
	body, err := json.Marshal(ArticlePayload{ArticleID: 42})
	require.NoError(t, err)

	id, err := decodeArticleID(asynq.NewTask(TaskArticleIndex, body))
	require.NoError(t, err)
	assert.Equal(t, uint64(42), id)
}

func TestDecodeArticleID_RejectsBadPayload(t *testing.T) {
	_, err := decodeArticleID(asynq.NewTask(TaskArticleIndex, []byte("not json")))
	require.Error(t, err)

	_, err = decodeArticleID(asynq.NewTask(TaskArticleIndex, []byte(`{}`)))
	require.Error(t, err, "缺少 article_id 必须报错，否则任务被静默吞掉")
}

func TestRetryDelay_LadderAndClamp(t *testing.T) {
	assert.Equal(t, 1*time.Second, retryDelay(0, nil, nil))
	assert.Equal(t, 1*time.Second, retryDelay(1, nil, nil))
	assert.Equal(t, 5*time.Second, retryDelay(2, nil, nil))
	assert.Equal(t, 30*time.Second, retryDelay(3, nil, nil))
	assert.Equal(t, 2*time.Minute, retryDelay(4, nil, nil))
	assert.Equal(t, 10*time.Minute, retryDelay(5, nil, nil))
	assert.Equal(t, 10*time.Minute, retryDelay(50, nil, nil), "超出档位后封顶")
}
