const BASE_RPC_URL = import.meta.env.VITE_RPC_BASE_URL;

const InstanceInfo = ({instance}) => {
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
					<input value={BASE_RPC_URL + "/eth/" + (instance.id ?? "")} disabled={true} />
					<input type="submit" value="Copy" />
				</div>
				<label htmlFor="rpcUrl">Setup Address</label>
				<div>
					<input value={"0x87B45dA8b51b7612Cb7b44b267f01860d4fF43bC"} disabled={true} />
					<input type="submit" value="Copy" />
				</div>
				<label htmlFor="rpcUrl">Player Private Key</label>
				<div>
					<input value={"0xb8c57cf2245c23279965b2e833a099a38d4fbd30fbfd8605d39eb502b8159e7d"} disabled={true} />
					<input type="submit" value="Copy" />
				</div>
			</div>
		</div>
		<div className="instance_body_disabled">
			<span>Instance not started</span>
		</div>
	</div>;
};

export default InstanceInfo;