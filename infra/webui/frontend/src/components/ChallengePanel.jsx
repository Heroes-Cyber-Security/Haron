import { useEffect, useState } from "react";
import { apiClient } from "../App";
import Markdown from "./Markdown";

async function create(token, challenge) {}

const ChallengePanel = ({account, instance, setInstance, setFlag}) => {
	const [challengeHash, setChallenge] = useState("");
	const [readme, setReadme] = useState("");
	const [challenges, setChallenges] = useState([]);
	const [readmes, setReadmes] = useState([]);

	useEffect(() => {
		(async () => {
			const res = await apiClient.get('/challenges');
			const data = res.data;
			if (data.error) {
				console.error(data);
				return;
			}

			setChallenges(data.challenges);
			setReadmes(data.readmes);

			setChallenge(data.challenges[0]);
			setReadme(data.readmes[0]);
		})();
	}, []);

	if (!account.name) {
		return <div className="challenge_panel unauthorized">
			<span>Please log in first</span>
		</div>;
	}

	const onChallengeChange = async (e) => {
		setChallenge(e.target.value);

		for (let i = 0; i < challenges.length; i++) {
			if (challenges[i] == e.target.value) {
				setReadme(readmes[i]);
				break;
			}
		}
	};

	const handleStart = async (e) => {
		setInstance(x => ({
			starting: true
		}));
		const res = await apiClient.post('/create', undefined, {
			headers: {
				Token: account.accessToken,
				Challenge: challengeHash
			}
		});

		const data = res.data;
		setInstance(x => ({
			id: data.id
		}));
	}

	const handleStop = async (e) => {
		if (!instance.id) return;

		const res = apiClient.post('/stop', undefined, {
			headers: {
				Token: account.accessToken
			}
		});
		setInstance(x => ({}));
	}

	const handleFlag = async (e) => {
		if (!instance.id) return;
		
		const res = await apiClient.get('/flag', {
			headers: {
				Token: account.accessToken
			}
		});
		const data = res.data;
		const flag = data.flag;

		setFlag(flag);
	}

	return <div className="challenge_panel">
		<div>
			<div>
				<select onChange={onChallengeChange} disabled={!!(instance.id)}>
					{challenges.filter(x => x).map(x => <option key={x} value={x}>{x}</option>)}
				</select>
			</div>
			{/* { !!(instance.id) ? <small>You already have a running instance</small> : null } */}
			<div className="challenge_readme">
				<Markdown content={readme} />
			</div>
		</div>
		<div>
			<form className="control_panel" onSubmit={e => e.preventDefault()}>
				<input
					type="submit"
					value={instance.starting ? "Starting..." : "Start"}
					disabled={!!instance.id || !!instance.starting}
					onClick={handleStart}
				/>
				<input type="submit" value="Stop" disabled={!instance.id} onClick={handleStop} />
				<input type="submit" value="Flag" disabled={!instance.id} onClick={handleFlag} />
			</form>
		</div>
	</div>;
};

export default ChallengePanel;