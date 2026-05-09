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

    # Grid strain detected AND node has excess power.
    if freq < 49.8 and battery > 50.0:
        energy_provisioned = battery * 0.1  # Discharge 10% of battery.
        return {
            "type": "EXECUTE_TRADE",
            "trade_id": str(uuid.uuid4()),
            "contributor_id": data["node_id"],
            "energy_kw": energy_provisioned,
            "credit": energy_provisioned * 0.15,  # $0.15 per kW.
            "freq": freq,
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
