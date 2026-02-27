from typing import Dict, Optional, Tuple


def precompile() -> Dict:
    """
    EXPERIMENTAL
    """
    return dict()


def genesis() -> Optional[Dict]:
    """
    Returns a genesis.json file
    """
    return None


def on_new_block():
    """
    Triggered every time a new block has been mined
    """
    pass


def on_incoming_tx() -> Tuple[int, str]:
    """
    Triggered every time a new tx is proposed to the mempool

    Returns:
     - int: HTTP status code, with 4xx dropping the incoming tx
     - str: Error message when tx is dropped
    """
    return 200, ""


def on_tx():
    """
    Triggered every time a transaction has been included in a block
    """
    pass


def is_solved() -> bool:
    """
    Returns True if the challenge has been solved
    """
    return True
