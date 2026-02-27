import {useEffect} from 'react';
import { apiClient } from '../App';

async function getAccount(accessToken) {
	const res = await apiClient.get('/profile?accessToken=' + accessToken);
	const data = res.data;
	if (!data.data) return;
	return data.data;
}

const Authentication = ({account, setAccount}) => {
	const handleSubmit = async (e) => {
		e.preventDefault();

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
		}
	};

	if (!account.name) {
		return <form style={{"height": "100%"}} onSubmit={handleSubmit}>
			<div style={{"padding": "1.5em"}}>
				<div className="input-group">
					<label htmlFor="accessToken">Access Token</label>
					<input name="accessToken" placeholder="..." />
				</div>
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