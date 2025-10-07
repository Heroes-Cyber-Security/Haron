import { useEffect, useState } from "react";
import { apiClient } from "../App";

async function create(token, challenge) {}

const ChallengePanel = ({account, instance, setInstance}) => {
	const [challengeHash, setChallenge] = useState("");
	const [challenges, setChallenges] = useState([]);


	useEffect(() => {
		(async () => {
			const res = await apiClient.get('/challenges');
			const data = res.data;
			if (data.error) {
				console.error(data);
				return;
			}

			setChallenges(data.challenges);
		})();
	}, []);

	if (!account.name) {
		return <div className="challenge_panel unauthorized">
			<span>Please log in first</span>
		</div>;
	}

	const onChallengeChange = async (e) => {
		setChallenge(e.target.value);

		// TODO: Add challenge desc as sibling to <select>
	};

	const handleStart = async (e) => {
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
		
	}

	const handleFlag = async (e) => {
		
	}

	return <div className="challenge_panel">
		<div>
			<select onChange={onChallengeChange}>
				{challenges.filter(x => x).map(x => <option key={x} value={x}>{x}</option>)}
			</select>
		</div>
		<div>
			<form className="control_panel" onSubmit={e => e.preventDefault()}>
				<input type="submit" value="Start" onClick={handleStart} />
				<input type="submit" value="Stop" onClick={handleStop} />
				<input type="submit" value="Flag" onClick={handleFlag} />
			</form>
		</div>
	</div>;
};

export default ChallengePanel;