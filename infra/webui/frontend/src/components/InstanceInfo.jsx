import {useState} from 'react';
import {copyToClipboard} from '../lib/clipboard';

const BASE_RPC_URL = import.meta.env.VITE_RPC_BASE_URL ?? `http://${window.location.host}`;

const InstanceInfo = ({instance}) => {
	const [selectedChainIndex, setSelectedChainIndex] = useState(0);

	const isMultiChain = instance.chains && instance.chains.length > 1;

	const currentChain = isMultiChain
		? instance.chains[selectedChainIndex]
		: null;

	const rpcUrl = currentChain?.rpc ?? (BASE_RPC_URL + "/eth/" + (instance.id ?? ""));
	const setupAddress = currentChain?.setup_address ?? instance.setup_address ?? "";
	const playerPrivateKey = instance.player_private_key ?? "";

	return <div className="instance_container" disabled={!instance.id} >
		<div className="instance_header">
			<span>Instance Information</span>
			{isMultiChain && (
				<select
					value={selectedChainIndex}
					onChange={e => setSelectedChainIndex(Number(e.target.value))}
				>
					{instance.chains.map((chain, i) => (
						<option key={chain.chainId} value={i}>
							{chain.name || `Chain ${chain.chainId}`}
						</option>
					))}
				</select>
			)}
		</div>
		<div className="instance_body_container">
			<div className="instance_body">
				<label htmlFor="rpcUrl">RPC</label>
				<div>
					<input value={rpcUrl} disabled={true} />
					<input type="submit" value="Copy" onClick={() => copyToClipboard(rpcUrl)} />
				</div>
				<label htmlFor="setupAddress">Setup Address</label>
				<div>
					<input value={setupAddress} disabled={true} />
					<input type="submit" value="Copy" onClick={() => copyToClipboard(setupAddress)} />
				</div>
				<label htmlFor="privateKey">Player Private Key</label>
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