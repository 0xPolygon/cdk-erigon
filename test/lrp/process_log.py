import re
import argparse
import logging
from datetime import datetime

logging.basicConfig(level=logging.INFO, format='%(message)s')

def parse_time(line):
    match = re.search(r'\[(\d{2}-\d{2}\|\d{2}:\d{2}:\d{2}\.\d{3})\]', line)
    if match:
        current_year = datetime.now().year
        parsed_time = datetime.strptime(match.group(1), '%m-%d|%H:%M:%S.%f')
        return parsed_time.replace(year=current_year)
    return None

def main(log_file):
    last_batch = None
    start_time = None
    end_time = None
    tx_count = 0

    with open(log_file, 'r') as file:
        lines = file.readlines()

        if len(lines) < 5:
            logging.error("Log file does not contain enough lines.")
            return

        first_line = None
        for i, line in enumerate(lines):
            if "[5/13 Execution] Last batch" in line:
                first_line = line
                break

        if not first_line:
            logging.error("Last batch line not found.")
            return

        last_batch = int(re.search(r'Last batch (\d+)', first_line).group(1))
        halt_batch = int(re.search(r'resequencing from batch \d+ to (\d+)', first_line).group(1))

        first_line_time = parse_time(first_line)
        second_line_time = parse_time(lines[i+1])
        startup_duration = (second_line_time - first_line_time).total_seconds()

        for i, line in enumerate(lines):
            if "Resequence from batch" in line and "in data stream" in line:
                start_time = parse_time(line)
                break

        if not start_time:
            logging.error("Start time not found.")
            return

        total_tx_count = 0
        for line in lines[i:]:
            if "Batch" in line and "TotalDuration-batch" in line and "Txs" in line:
                batch_match = re.search(r'Batch<(\d+)>', line)
                duration_match = re.search(r'TotalDuration-batch<(\d+)ms>', line)
                tx_match = re.search(r'Txs<(\d+)>', line)
                
                if batch_match and duration_match and tx_match:
                    batch_no = int(batch_match.group(1))
                    duration_ms = int(duration_match.group(1))
                    tx_count = int(tx_match.group(1))
                    total_tx_count += tx_count
                    
                    instant_tps = (tx_count * 1000) / duration_ms if duration_ms > 0 else 0
                    logging.info(f"Batch {batch_no}: {tx_count} txs in {duration_ms/1000:.3f} seconds, Instant TPS: {instant_tps:.2f}")
            
            if "Replay completed." in line:
                end_time = parse_time(line)
                break

        if not end_time:
            logging.error("End time not found.")
            return

        duration = (end_time - start_time).total_seconds()
        avg_tps = total_tx_count / duration if duration > 0 else 0

        logging.info("")
        logging.info(f"{'From Batch:':<25} {last_batch+1}")
        logging.info(f"{'To Batch:':<25} {halt_batch}")
        logging.info(f"{'Data Stream Startup:':<25} {startup_duration} seconds")
        logging.info(f"{'Start Time:':<25} {start_time}")
        logging.info(f"{'End Time:':<25} {end_time}")
        logging.info(f"{'Total Transactions:':<25} {total_tx_count}")
        logging.info(f"{'Replay Duration:':<25} {duration} seconds")
        logging.info(f"{'Average TPS:':<25} {avg_tps}")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description='Process log file to calculate TPS.')
    parser.add_argument('log_file', type=str, help='Path to the log file')
    args = parser.parse_args()
    main(args.log_file)