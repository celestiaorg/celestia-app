use crate::tx::TxResult;

#[derive(Debug, Default)]
pub struct Statistics {
    pub total_count: usize,
    pub success_count: usize,
    pub failure_count: usize,
    pub mean_latency_ms: f64,
    pub std_dev_ms: f64,
}

impl Statistics {
    pub fn calculate(results: &[TxResult]) -> Self {
        let total_count = results.len();
        let mut success_count = 0;
        let mut failure_count = 0;
        let mut latencies = Vec::new();

        for result in results {
            if result.failed {
                failure_count += 1;
            } else {
                success_count += 1;
                latencies.push(result.latency.as_millis() as f64);
            }
        }

        let (mean_latency_ms, std_dev_ms) = Self::compute_stats(&latencies);

        Self {
            total_count,
            success_count,
            failure_count,
            mean_latency_ms,
            std_dev_ms,
        }
    }

    fn compute_stats(latencies: &[f64]) -> (f64, f64) {
        if latencies.is_empty() {
            return (0.0, 0.0);
        }

        let sum: f64 = latencies.iter().sum();
        let mean = sum / latencies.len() as f64;

        // Using sample variance (n-1) for better statistical accuracy
        let variance: f64 = if latencies.len() > 1 {
            latencies.iter().map(|l| (l - mean).powi(2)).sum::<f64>() / (latencies.len() - 1) as f64
        } else {
            0.0
        };
        let std_dev = variance.sqrt();

        (mean, std_dev)
    }

    pub fn success_rate(&self) -> f64 {
        if self.total_count == 0 {
            0.0
        } else {
            (self.success_count as f64 / self.total_count as f64) * 100.0
        }
    }

    pub fn failure_rate(&self) -> f64 {
        if self.total_count == 0 {
            0.0
        } else {
            (self.failure_count as f64 / self.total_count as f64) * 100.0
        }
    }

    pub fn print(&self) {
        println!("\nTransaction Statistics:");
        println!("Total transactions: {}", self.total_count);
        println!(
            "Successful: {} ({:.1}%)",
            self.success_count,
            self.success_rate()
        );
        println!(
            "Failed: {} ({:.1}%)",
            self.failure_count,
            self.failure_rate()
        );

        if self.success_count > 0 {
            println!("\nLatency Statistics (successful transactions only):");
            println!("Average latency: {:.2} ms", self.mean_latency_ms);
            println!("Standard deviation: {:.2} ms", self.std_dev_ms);
        }
    }
}
