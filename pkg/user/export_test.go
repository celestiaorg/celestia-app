package user

// QueuedJobsForTest returns the number of jobs buffered in the tx queue.
func (client *TxClient) QueuedJobsForTest() int {
	return len(client.txQueue.jobQueue)
}
