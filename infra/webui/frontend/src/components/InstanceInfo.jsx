import {copyToClipboard} from '../lib/clipboard';

const BASE_RPC_URL = import.meta.env.VITE_RPC_BASE_URL ?? `http://${window.location.host}`;

const InstanceInfo = ({instance}) => {
	const rpcUrl = BASE_RPC_URL + "/eth/" + (instance.id ?? "");
	const setupAddress = "0x87B45dA8b51b7612Cb7b44b267f01860d4fF43bC";
	const playerPrivateKey = "0xb8c57cf2245c23279965b2e833a099a38d4fbd30fbfd8605d39eb502b8159e7d";

	return <div className="instance_container" disabled={!instance.id} >
		<div className="instance_header">
			<span>Instance Information</span>
			<select>
				<option>Instance A</option>
			</select>
		</div>
		<div className="instance_body_container">
			<div className="instance_body">
				<label htmlFor="rpcUrl">RPC</label>
				<div>
					<input value={rpcUrl} disabled={true} />
					<input type="submit" value="Copy" onClick={() => copyToClipboard(rpcUrl)} />
				</div>
				<label htmlFor="rpcUrl">Setup Address</label>
				<div>
					<input value={setupAddress} disabled={true} />
					<input type="submit" value="Copy" onClick={() => copyToClipboard(setupAddress)} />
				</div>
				<label htmlFor="rpcUrl">Player Private Key</label>
				<div>
					<input value={playerPrivateKey} disabled={true} />
					<input type="submit" value="Copy" onClick={() => copyToClipboard(playerPrivateKey)} />
				</div>
			</div>
		</div>
		<div className="instance_body_disabled">
			<span>Instance not started</span>
		</div>
	</div>;
};

export default InstanceInfo;