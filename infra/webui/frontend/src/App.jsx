import {useEffect, useState} from 'react';
import axios from 'axios';

import Authentication from './components/Authentication';
import ChallengePanel from './components/ChallengePanel';

export const apiClient = axios.create({
	baseURL: import.meta.env.VITE_API_BASE_URL ?? '/api',
	timeout: 10_000
});

const App = () => {
	const [status, setStatus] = useState({loading: true, error: null, data: null});
	const [account, setAccount] = useState({name: "", accessToken: ""});
	const [instance, setInstance] = useState({id: ""});

	useEffect(() => {
		
	}, []);

	return (
		<div className="app">
			<div className="launcher">
				<div className="div1" style={{"padding": "0"}}>
					<ChallengePanel account={account} instance={instance} setInstance={setInstance} />
				</div>
				<div className="div2">{instance.id}</div>
				<div className="div3">3</div>
				<div className="div4" style={{"padding": "0"}}>
					<Authentication account={account} setAccount={setAccount}/>
				</div>
			</div>
		</div>
	);
};

export default App;
