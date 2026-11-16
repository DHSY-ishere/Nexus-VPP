import json
import uuid
from datetime import datetime
from confluent_kafka import Consumer, Producer

conf_consumer = {
    "bootstrap.servers": "127.0.0.1:9092",
    "group.id": "ml-logic",
    "auto.offset.reset": "latest",
}
conf_producer = {"bootstrap.servers": "127.0.0.1:9092"}

consumer = Consumer(conf_consumer)
producer = Producer(conf_producer)
consumer.subscribe(["nexus.telemetry.raw"])


def evaluate_p2p_trade(data):
    freq = data.get("local_frequency_hz", 50.0)
    battery = data.get("state_of_charge_percent", 0.0)
    consumption = data.get("current_consumption_kw", 0.0)
    replenishment = data.get("replenishment_rate_kw", 0.0)

    # Provisioning Criteria (Peak vs. Non-Peak / Daylight vs. Moonlight)
    current_hour = datetime.utcnow().hour
    is_daylight = 6 <= current_hour < 18
    is_peak_replenishment = 10 <= current_hour < 16

    # Peak Replenishment Period & Consumer Threshold Calculations
    if is_peak_replenishment:
        threshold = 40.0  # Lower threshold during peak solar
        discharge_multiplier = 0.15
    elif not is_daylight: # Moonlight/Night
        threshold = 60.0  # Higher threshold to conserve battery
        discharge_multiplier = 0.05
    else: # Daylight non-peak
        threshold = 50.0
        discharge_multiplier = 0.10

    # Grid strain detected AND node has excess power based on dynamic threshold.
    if freq < 49.8 and battery > threshold:
        # Dynamic Storage Energy Contribution Calculation
        # Dynamically decided by current consumption and replenishment rate
        battery_contribution = battery * discharge_multiplier
        net_energy = replenishment - consumption
        
        if net_energy > 0:
            energy_provisioned = battery_contribution + (net_energy * 0.8)
        else:
            # Limit the discharge if consuming more than replenishing
            energy_provisioned = max(1.0, battery_contribution + net_energy)
            
        return {
            "type": "EXECUTE_TRADE",
            "trade_id": str(uuid.uuid4()),
            "contributor_id": data["node_id"],
            "energy_kw": energy_provisioned,
            "credit": energy_provisioned * 0.15,  # $0.15 per kW.
            "grid_frequency": freq,
            "timestamp": datetime.utcnow().isoformat(),
        }
    return None


print("ML Engine hunting for P2P arbitration opportunities...")
while True:
    msg = consumer.poll(1.0)
    if msg is None:
        continue
    if msg.error():
        continue

    telemetry = json.loads(msg.value().decode("utf-8"))
    trade_command = evaluate_p2p_trade(telemetry)

    if trade_command:
        producer.produce("nexus.commands", json.dumps(trade_command).encode("utf-8"))
        producer.flush()
        print(f"P2P Trade Executed for {trade_command['contributor_id']}")
