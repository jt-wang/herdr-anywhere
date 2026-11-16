import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('evidence', Path(__file__).with_name('layout-race-evidence.py'))
evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(evidence)


class LayoutEvidenceTest(unittest.TestCase):
    def setUp(self):
        self.visible = dict(stage='visible', terminal='t2', originalPane='pnew', pane='pnew', focused='pnew',
                            conflicts=1, runs=1, beforeVersion=1, version=2)
        self.released = dict(self.visible, stage='released', rename='grove', documentRename='grove', documentPanes=['pnew'])

    def test_preserved_terminal_passes(self):
        self.assertEqual(evidence.check(self.visible, self.released), 0)

    def test_lost_original_pane_is_the_expected_product_red(self):
        self.released.update(pane='padopted', focused='pold', documentPanes=['padopted'])
        self.assertEqual(evidence.check(self.visible, self.released), 1)

    def test_missing_conflict_is_not_a_product_red(self):
        self.released.update(conflicts=0, pane=None)
        with self.assertRaises(ValueError): evidence.check(self.visible, self.released)

    def test_missing_initial_visible_pane_is_not_a_product_red(self):
        self.visible['pane'] = None
        with self.assertRaises(ValueError): evidence.check(self.visible, self.released)

    def test_input_must_reach_the_preserved_terminal(self):
        sent = dict(self.released, stage='input', input=['t2', 'layout input ok'], screen='layout input ok')
        self.assertEqual(evidence.check(self.visible, self.released, sent), 0)
        sent['input'][0] = 't1'
        with self.assertRaises(ValueError): evidence.check(self.visible, self.released, sent)


if __name__ == '__main__': unittest.main()
