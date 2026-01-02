use crate::config::Result;
use crate::metrics::Statistics;
use crate::tx::TxResult;

pub fn output_results(results: &[TxResult]) -> Result<()> {
    write_results_to_csv(results)?;

    let stats = Statistics::calculate(results);
    stats.print();

    println!("\nResults written to latency_results.csv");
    Ok(())
}

fn write_results_to_csv(results: &[TxResult]) -> Result<()> {
    let file = std::fs::File::create("latency_results.csv")?;
    let mut writer = csv::Writer::from_writer(file);

    write_csv_header(&mut writer)?;

    for result in results {
        write_csv_row(&mut writer, result)?;
    }

    writer.flush()?;
    Ok(())
}

fn write_csv_header<W: std::io::Write>(writer: &mut csv::Writer<W>) -> Result<()> {
    writer.write_record([
        "Submit Time",
        "Commit Time",
        "Latency (ms)",
        "Tx Hash",
        "Height",
        "Code",
        "Failed",
        "Error",
    ])?;
    Ok(())
}

fn write_csv_row<W: std::io::Write>(writer: &mut csv::Writer<W>, result: &TxResult) -> Result<()> {
    let latency_str = if result.failed {
        String::new()
    } else {
        format!("{:.2}", result.latency.as_millis() as f64)
    };

    writer.write_record([
        humantime::format_rfc3339(result.submit_time).to_string(),
        humantime::format_rfc3339(result.commit_time).to_string(),
        latency_str,
        result.tx_hash.clone(),
        result.height.to_string(),
        result.code.to_string(),
        result.failed.to_string(),
        result.error_msg.clone(),
    ])?;

    Ok(())
}
