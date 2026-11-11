"""Author a body and four wheels in Blender, with no imported donor geometry."""
import math
import os
import sys

import bpy

bpy.ops.wm.read_factory_settings(use_empty=True)
bpy.ops.mesh.primitive_cube_add(size=1, location=(0, 0, 0.75))
body = bpy.context.object
body.name = "HB"
body.dimensions = (2, 4, 1.5)
for name, x, y in [
    ("HLFW", -1.1, 1.3), ("HRFW", 1.1, 1.3),
    ("HLRW", -1.1, -1.3), ("HRRW", 1.1, -1.3),
]:
    bpy.ops.mesh.primitive_cylinder_add(
        vertices=12, radius=0.4, depth=0.2,
        location=(x, y, 0.4), rotation=(0, math.pi / 2, 0),
    )
    bpy.context.object.name = name
bpy.ops.wm.obj_export(
    filepath=os.path.join(sys.argv[1], "new_car.obj"),
    forward_axis="NEGATIVE_Z", up_axis="Y",
    export_uv=True, export_normals=True, export_materials=False,
    export_triangulated_mesh=True,
)
print("Created a new five-part model in Blender", bpy.app.version_string)
