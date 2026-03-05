import {useEffect, useState} from 'react';
import { apiClient } from '../App';

async function getAccount(accessToken) {
	const res = await apiClient.get('/profile?accessToken=' + accessToken);
	const data = res.data;
	if (!data.data) return;
	if (data.data.id === -1 || data.data.name === "invalid") return;
	return data.data;
}

const Authentication = ({account, setAccount}) => {
	const [error, setError] = useState("");

	const handleSubmit = async (e) => {
		e.preventDefault();
		setError("");

		if (account.name) {
			setAccount((x) => ({}));
			return;
		}

		let accessToken = e.target[0].value;
		let _account = await getAccount(accessToken);

		if (_account) {
			setAccount((x) => ({
				name: _account.name,
				accessToken: accessToken
			}));
		} else {
			setError("Authentication failed");
		}
	};

	if (!account.name) {
		return <form style={{"height": "100%"}} onSubmit={handleSubmit}>
			<div style={{"padding": "1.5em"}}>
				<div className="input-group">
					<label htmlFor="accessToken">Access Token</label>
					<input name="accessToken" placeholder="..." />
				</div>
				{error && <div className="error" style={{"color": "red", "marginTop": "1em"}}>{error}</div>}
			</div>
			<input type="submit" value="Log In" />
		</form>;
	} else {
		return <form style={{"height": "100%"}} onSubmit={handleSubmit}>
			<div style={{"padding": "1.5em"}}>
				<div className="input-group">
					<label>Hi {account.name}</label>
				</div>
			</div>
			<input type="submit" value="Log Out" />
		</form>;
	}
};

export default Authentication;